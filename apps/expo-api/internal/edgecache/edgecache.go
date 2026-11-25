// Package edgecache purges Cloudflare's copies of preview images and raw
// exchange strings when an entry
// stops being public (made private, deleted, expired), so they do not outlive
// it by the images' max-age. Without a token and zone id it does nothing.
package edgecache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	apiBase = "https://api.cloudflare.com/client/v4"
	// filesPerCall is Cloudflare's limit of URLs in one purge request.
	filesPerCall = 30
	purgeTimeout = 10 * time.Second
)

// Purger purges preview URLs of one site through Cloudflare's API. The zero
// value and a nil *Purger are disabled.
type Purger struct {
	origin  string
	zoneID  string
	token   string
	apiBase string
	client  *http.Client
}

// New builds a purger from API_CLOUDFLARE_API_TOKEN and
// API_CLOUDFLARE_ZONE_ID (token and zone). It is nil, hence disabled, unless
// both are set (a missing token is not an error: local and staging setups
// have no zone). origin is the site's origin, where the previews' public URLs
// live.
func New(origin, token, zone string) *Purger {
	if token == "" || zone == "" {
		slog.Info("Cloudflare preview purge off (API_CLOUDFLARE_API_TOKEN or API_CLOUDFLARE_ZONE_ID unset)")
		return nil
	}
	slog.Info("Cloudflare preview purge on", "origin", origin)
	return &Purger{
		origin: strings.TrimRight(origin, "/"), zoneID: zone, token: token,
		apiBase: apiBase, client: &http.Client{Timeout: purgeTimeout},
	}
}

// Enabled reports whether purges are sent.
func (p *Purger) Enabled() bool {
	return p != nil && p.token != "" && p.zoneID != ""
}

// PurgeEntry purges the cached URLs of one entry: each blueprint's full
// preview, thumbnail and raw string, each revision's raw string and the
// entry's own. It returns at once; the requests run in the background and a
// failure is only logged (the max-age bounds what a missed purge costs).
func (p *Purger) PurgeEntry(entryID string, revisionIDs, blueprintIDs []string) {
	if !p.Enabled() {
		return
	}
	files := p.entryURLs(entryID, revisionIDs, blueprintIDs)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), purgeTimeout*time.Duration(len(files)/filesPerCall+1))
		defer cancel()
		if err := p.purge(ctx, files); err != nil {
			slog.Warn("Cloudflare preview purge failed", "files", len(files), "error", err)
		}
	}()
}

func (p *Purger) entryURLs(entryID string, revisionIDs, blueprintIDs []string) []string {
	entry := p.origin + "/api/v1/entries/" + url.PathEscape(entryID)
	files := []string{entry + "/raw"}
	for _, id := range revisionIDs {
		files = append(files, entry+"/revisions/"+url.PathEscape(id)+"/raw")
	}
	for _, id := range blueprintIDs {
		blueprint := p.origin + "/api/v1/blueprints/" + url.PathEscape(id)
		files = append(files, blueprint+"/preview.webp", blueprint+"/thumbnail.webp", blueprint+"/raw")
	}
	return files
}

func (p *Purger) purge(ctx context.Context, files []string) error {
	for start := 0; start < len(files); start += filesPerCall {
		batch := files[start:min(start+filesPerCall, len(files))]
		if err := p.purgeBatch(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func (p *Purger) purgeBatch(ctx context.Context, files []string) error {
	body, err := json.Marshal(map[string][]string{"files": files})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.apiBase+"/zones/"+url.PathEscape(p.zoneID)+"/purge_cache", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("cloudflare answered %d: %s", response.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}
