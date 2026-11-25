package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// TypesenseClient is the real search.Client backend: a small, dependency-free
// wrapper over Typesense's HTTP document/collection API (no SDK — this
// project's services talk to their dependencies over plain net/http). It
// only stores and queries documents; the Syncer builds them.
type TypesenseClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	// names are this build's versioned collections: every write goes there.
	names collectionNames
	// queried are the collections searches read: the published version while
	// this build's is still being built (Rebuild), then names.
	queried atomic.Pointer[collectionNames]
}

// NewTypesenseClient builds a Client backed by a running Typesense server at
// baseURL (e.g. "http://expo-typesense:8108"), authenticating with apiKey. A
// nil httpClient defaults to a client with a bounded per-request timeout, so
// a stalled search node can't hang the dispatcher or an HTTP request forever.
func NewTypesenseClient(baseURL, apiKey string, httpClient *http.Client) *TypesenseClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	client := &TypesenseClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: httpClient,
		names:      namesForVersion(indexVersion()),
	}
	client.queried.Store(&client.names)
	return client
}

// writeProjection replaces every document of the entry. Documents are
// replaced wholesale (delete-by-filter, then bulk import) rather than diffed,
// so a result removed by a new revision can't leave a stale document behind.
func (c *TypesenseClient) writeProjection(ctx context.Context, proj projection) error {
	if err := c.deleteByFilter(ctx, c.names.Blueprints, "entry_id:="+proj.Entry.ID); err != nil {
		return fmt.Errorf("clear stale documents: %w", err)
	}
	if len(proj.Blueprints) == 0 {
		return nil
	}
	docs := make([]map[string]any, 0, len(proj.Blueprints))
	for _, doc := range proj.Blueprints {
		m, err := doc.toMap()
		if err != nil {
			return fmt.Errorf("encode document: %w", err)
		}
		docs = append(docs, m)
	}
	if err := c.importDocuments(ctx, c.names.Blueprints, docs); err != nil {
		return fmt.Errorf("import documents: %w", err)
	}
	return nil
}

// removeEntry removes the entry's documents. It tolerates them already being
// absent, since the dispatcher may retry a delete that partially succeeded.
func (c *TypesenseClient) removeEntry(ctx context.Context, entryID string) error {
	if err := c.deleteByFilter(ctx, c.names.Blueprints, "entry_id:="+entryID); err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}
	return nil
}

// updateViewCounts writes view_count into every document of each entry
// (partial updates by filter). Documents that are gone by now (the entry was
// deleted or made non-public meanwhile) match nothing.
func (c *TypesenseClient) updateViewCounts(ctx context.Context, counts []viewCount) error {
	for _, count := range counts {
		body, err := json.Marshal(map[string]int64{"view_count": count.ViewCount})
		if err != nil {
			return err
		}
		status, respBody, err := c.do(ctx, http.MethodPatch, "/collections/"+c.names.Blueprints+"/documents",
			url.Values{"filter_by": {"entry_id:=" + count.ID}}, body)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("update view counts: %s", describeError(status, respBody))
		}
	}
	return nil
}

// importDocuments bulk-upserts docs via Typesense's JSONL import endpoint.
func (c *TypesenseClient) importDocuments(ctx context.Context, collection string, docs []map[string]any) error {
	var buf bytes.Buffer
	for _, doc := range docs {
		line, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	status, respBody, err := c.do(ctx, http.MethodPost, "/collections/"+collection+"/documents/import", url.Values{"action": {"upsert"}}, buf.Bytes())
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("import documents: %s", describeError(status, respBody))
	}
	return checkImportResult(respBody)
}

// checkImportResult checks the body of an import. Typesense reports per-row
// failures with 200 OK and a JSONL body of {"success":bool,...} lines, so a
// 200 status alone doesn't mean every row succeeded.
func checkImportResult(body []byte) error {
	for line := range strings.SplitSeq(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var result struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			return fmt.Errorf("decode import result: %w", err)
		}
		if !result.Success {
			return fmt.Errorf("import row failed: %s", result.Error)
		}
	}
	return nil
}

func (c *TypesenseClient) deleteByFilter(ctx context.Context, collection, filter string) error {
	status, respBody, err := c.do(ctx, http.MethodDelete, "/collections/"+collection+"/documents", url.Values{"filter_by": {filter}}, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("delete documents by filter: %s", describeError(status, respBody))
}

// do issues one Typesense API request and returns the response status and
// body. It never treats a non-2xx status as a Go error itself, so callers
// can distinguish "not found" (often fine to ignore) from a real failure.
func (c *TypesenseClient) do(ctx context.Context, method, path string, query url.Values, body []byte) (int, []byte, error) {
	full := c.baseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reqBody)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-TYPESENSE-API-KEY", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request typesense: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read typesense response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

func describeError(status int, body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		return strconv.Itoa(status) + ": " + payload.Message
	}
	return strconv.Itoa(status) + ": " + string(body)
}
