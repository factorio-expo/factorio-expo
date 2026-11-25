package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
)

// EnsureIndex makes this build's versioned collections the live search index.
// The Keeper runs it in the background; searches keep reading the published
// version meanwhile.
//
// The "blueprints" alias pointing at this version's collection marks a
// completed build: the entry_search_jobs outbox has kept it current since, so
// a restart with an unchanged schema rebuilds nothing (it only drops versions a
// crash after publishing left behind). Otherwise (first start, schema or
// projectionVersion change, lost Typesense data, a build interrupted mid-way)
// it rebuilds (Rebuild), searching the previously published version until the
// new one is live. It returns whether it rebuilt and how many entries it
// indexed.
func (c *TypesenseClient) EnsureIndex(ctx context.Context, syncer *Syncer) (bool, int, error) {
	live, published, err := c.indexLive(ctx)
	if err != nil {
		return false, 0, err
	}
	if live {
		return false, 0, c.dropStale(ctx)
	}
	if version, ok := strings.CutPrefix(published, "blueprints_"); ok {
		previous := namesForVersion(version)
		c.queried.Store(&previous)
	}
	indexed, err := c.Rebuild(ctx, syncer)
	return err == nil, indexed, err
}

// Rebuild recreates this version's collections, indexes every searchable
// entry from PostgreSQL, points the aliases at the new collections and drops
// every other version. The search dispatcher must not run meanwhile: an entry
// it synced while the rebuild read an older state could be overwritten with
// that state. Intents wait in entry_search_jobs until it is restarted.
func (c *TypesenseClient) Rebuild(ctx context.Context, syncer *Syncer) (int, error) {
	if err := c.createCollections(ctx); err != nil {
		return 0, err
	}
	indexed, err := syncer.IndexAll(ctx)
	if err != nil {
		return 0, err
	}
	if err := c.publish(ctx); err != nil {
		return 0, err
	}
	return indexed, nil
}

// IndexVersion is the schema/projection fingerprint used in collection names.
func (c *TypesenseClient) IndexVersion() string {
	return c.names.Version
}

// indexLive reports whether the blueprints alias points at this version's
// collection and that collection exists. published is the alias
// target ("" without an alias).
func (c *TypesenseClient) indexLive(ctx context.Context) (live bool, published string, err error) {
	status, body, err := c.do(ctx, http.MethodGet, "/aliases/blueprints", nil, nil)
	if err != nil {
		return false, "", fmt.Errorf("read blueprints alias: %w", err)
	}
	if status == http.StatusNotFound {
		return false, "", nil
	}
	if status != http.StatusOK {
		return false, "", fmt.Errorf("read blueprints alias: %s", describeError(status, body))
	}
	var alias struct {
		CollectionName string `json:"collection_name"`
	}
	if err := json.Unmarshal(body, &alias); err != nil {
		return false, "", fmt.Errorf("decode blueprints alias: %w", err)
	}
	if alias.CollectionName != c.names.Blueprints {
		return false, alias.CollectionName, nil
	}
	_, exists, err := c.documentCount(ctx)
	return exists, alias.CollectionName, err
}

// documentCount returns how many documents this version's collection holds,
// and whether it exists.
func (c *TypesenseClient) documentCount(ctx context.Context) (int64, bool, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/collections/"+c.names.Blueprints, nil, nil)
	if err != nil {
		return 0, false, fmt.Errorf("read collection %q: %w", c.names.Blueprints, err)
	}
	switch status {
	case http.StatusOK:
		var collection struct {
			NumDocuments int64 `json:"num_documents"`
		}
		if err := json.Unmarshal(body, &collection); err != nil {
			return 0, false, fmt.Errorf("decode collection %q: %w", c.names.Blueprints, err)
		}
		return collection.NumDocuments, true, nil
	case http.StatusNotFound:
		return 0, false, nil
	}
	return 0, false, fmt.Errorf("read collection %q: %s", c.names.Blueprints, describeError(status, body))
}

// createCollections (re)creates this version's empty collections, dropping
// any left by an interrupted build first.
func (c *TypesenseClient) createCollections(ctx context.Context) error {
	schemas := c.names.schemas()
	if err := c.putSynonyms(ctx); err != nil {
		return err
	}
	for _, schema := range schemas {
		if err := c.dropCollection(ctx, schema.Name); err != nil {
			return err
		}
	}
	for _, schema := range schemas {
		if err := c.createCollection(ctx, schema); err != nil {
			return fmt.Errorf("create collection %q: %w", schema.Name, err)
		}
	}
	return nil
}

// indexEntries indexes each id, logging and counting entries that fail
// instead of aborting the build, and enqueues each failed entry as an index
// job so the dispatcher retries it. Only a build where every entry failed
// (the search engine itself is unusable) is fatal. It returns how many
// entries were indexed.
func indexEntries(ctx context.Context, ids []string, index func(context.Context, string) error, enqueue func(string) error) (int, error) {
	indexed := 0
	var failed []string
	var firstErr error
	for _, id := range ids {
		entryID := id
		if err := index(ctx, entryID); err != nil {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			slog.Error("index entry during rebuild", "entry_id", entryID, "error", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("index entry %s: %w", entryID, err)
			}
			failed = append(failed, entryID)
			if err := enqueue(id); err != nil {
				return 0, fmt.Errorf("enqueue retry for entry %s: %w", entryID, err)
			}
			continue
		}
		indexed++
	}
	if len(failed) > 0 {
		slog.Warn("entries failed to index during rebuild and were queued for retry", "failed", len(failed), "indexed", indexed)
	}
	if indexed == 0 && len(failed) > 0 {
		return 0, firstErr
	}
	return indexed, nil
}

// publish points the logical aliases at this version, switches searches to
// it, then drops every other version. The blueprints alias is the completion
// marker indexLive checks; a crash before the drops leaves old versions for
// the next EnsureIndex to drop. Legacy unversioned collections carry the
// alias names, so they go before the aliases are set (no search reads them).
func (c *TypesenseClient) publish(ctx context.Context) error {
	legacy, _, err := c.staleCollections(ctx)
	if err != nil {
		return err
	}
	if err := c.dropCollections(ctx, legacy); err != nil {
		return err
	}
	for _, schema := range slices.Backward(logicalSchemas) {
		body, err := json.Marshal(map[string]string{"collection_name": c.names.physical(schema.Name)})
		if err != nil {
			return err
		}
		status, respBody, err := c.do(ctx, http.MethodPut, "/aliases/"+schema.Name, nil, body)
		if err != nil {
			return fmt.Errorf("set alias %q: %w", schema.Name, err)
		}
		if status != http.StatusOK && status != http.StatusCreated {
			return fmt.Errorf("set alias %q: %s", schema.Name, describeError(status, respBody))
		}
	}
	c.queried.Store(&c.names)
	return c.dropStale(ctx)
}

// dropStale drops the other versions of the search collections.
func (c *TypesenseClient) dropStale(ctx context.Context) error {
	_, versions, err := c.staleCollections(ctx)
	if err != nil {
		return err
	}
	return c.dropCollections(ctx, versions)
}

func (c *TypesenseClient) dropCollections(ctx context.Context, names []string) error {
	for _, name := range names {
		if err := c.dropCollection(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// staleCollections lists the search collections that are not this version's:
// legacy unversioned ones (named like the aliases) and other versions, each
// in name order.
func (c *TypesenseClient) staleCollections(ctx context.Context) (legacy, versions []string, err error) {
	status, body, err := c.do(ctx, http.MethodGet, "/collections", nil, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("list collections: %w", err)
	}
	if status != http.StatusOK {
		return nil, nil, fmt.Errorf("list collections: %s", describeError(status, body))
	}
	var collections []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &collections); err != nil {
		return nil, nil, fmt.Errorf("decode collections: %w", err)
	}
	current := map[string]bool{c.names.Blueprints: true, c.names.Blueprints: true}
	for _, collection := range collections {
		name := collection.Name
		switch {
		case !isSearchCollection(name) || current[name]:
		case isLogicalName(name):
			legacy = append(legacy, name)
		default:
			versions = append(versions, name)
		}
	}
	slices.Sort(legacy)
	slices.Sort(versions)
	return legacy, versions, nil
}

func isLogicalName(name string) bool {
	return slices.ContainsFunc(logicalSchemas, func(schema collectionSchema) bool { return schema.Name == name })
}

func (c *TypesenseClient) dropCollection(ctx context.Context, name string) error {
	status, body, err := c.do(ctx, http.MethodDelete, "/collections/"+name, nil, nil)
	if err != nil {
		return fmt.Errorf("drop collection %q: %w", name, err)
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return fmt.Errorf("drop collection %q: %s", name, describeError(status, body))
	}
	return nil
}

func (c *TypesenseClient) createCollection(ctx context.Context, schema collectionSchema) error {
	body, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	status, respBody, err := c.do(ctx, http.MethodPost, "/collections", nil, body)
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		return nil
	}
	return errors.New(describeError(status, respBody))
}
