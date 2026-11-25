package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/catalog"
)

const maxCollectionRequestBytes = 1 << 20

// CollectionPath is the collection an operation is about.
type CollectionPath struct {
	CollectionID string `path:"collectionID" doc:"The collection's id."`
}

// collectionRequest is a collection's editable fields. Name is optional in
// the schema: an empty or over-long name is the catalog's invalid_collection.
type collectionRequest struct {
	Name        string `json:"name,omitempty" doc:"The collection's name (1 to 100 characters)."`
	Description string `json:"description,omitempty" doc:"The collection's description (at most 2000 characters)."`
	Visibility  string `json:"visibility,omitempty" doc:"public or private; omitted means private."`
}

type collectionEntryRequest struct {
	EntryID  string `json:"entry_id" minLength:"1" doc:"The entry to save, by its public id."`
	Position int32  `json:"position,omitempty" doc:"The position to place the entry at."`
}

type collectionOrderRequest struct {
	EntryIDs []string `json:"entry_ids,omitempty" doc:"The entry ids in display order. Members left out keep their relative place after the listed ones; ids that are not members are ignored."`
}

type collectionOutput struct {
	Body catalog.Collection
}

type collectionList struct {
	Collections []catalog.Collection `json:"collections" doc:"The caller's collections, newest first, without their entries."`
}

type collectionListOutput struct {
	Body collectionList
}

type collectionMembershipList struct {
	Collections []catalog.CollectionMembership `json:"collections" doc:"The caller's collections, newest first, each flagged with whether it holds the entry."`
}

type collectionMembershipOutput struct {
	Body collectionMembershipList
}

type createCollectionInput struct {
	Body collectionRequest
}

type updateCollectionInput struct {
	CollectionPath
	Body collectionRequest
}

type addCollectionEntryInput struct {
	CollectionPath
	Body collectionEntryRequest
}

type reorderCollectionEntriesInput struct {
	CollectionPath
	Body collectionOrderRequest
}

type removeCollectionEntryInput struct {
	CollectionPath
	EntryPath
}

// createCollection creates a new collection owned by the authenticated
// caller.
func (s *server) createCollection(ctx context.Context, in *createCollectionInput) (*collectionOutput, error) {
	collection, err := s.catalog.CreateCollection(
		ctx, viewerFrom(ctx), in.Body.Name, in.Body.Description, in.Body.Visibility,
	)
	if err != nil {
		return nil, err
	}
	return &collectionOutput{Body: collection}, nil
}

// listMyCollections lists every collection owned by the authenticated caller.
func (s *server) listMyCollections(ctx context.Context, _ *struct{}) (*collectionListOutput, error) {
	collections, err := s.catalog.ListCollections(ctx, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &collectionListOutput{Body: collectionList{Collections: collections}}, nil
}

// listEntryCollections lists the authenticated caller's collections, each
// flagged with whether it holds the entry, so the collection picker needs one
// request.
func (s *server) listEntryCollections(ctx context.Context, in *EntryPath) (*collectionMembershipOutput, error) {
	memberships, err := s.catalog.ListCollectionsForEntry(ctx, viewerFrom(ctx), in.EntryID)
	if err != nil {
		return nil, err
	}
	return &collectionMembershipOutput{Body: collectionMembershipList{Collections: memberships}}, nil
}

// getCollection returns one collection with its saved entries. Public
// collections are readable by anyone; private collections require the owner.
func (s *server) getCollection(ctx context.Context, in *CollectionPath) (*collectionOutput, error) {
	collection, err := s.catalog.GetCollection(ctx, in.CollectionID, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &collectionOutput{Body: collection}, nil
}

// updateCollection replaces a collection's name, description, and
// visibility. Only the owner may update it.
func (s *server) updateCollection(ctx context.Context, in *updateCollectionInput) (*collectionOutput, error) {
	collection, err := s.catalog.UpdateCollection(
		ctx, in.CollectionID, viewerFrom(ctx),
		in.Body.Name, in.Body.Description, in.Body.Visibility,
	)
	if err != nil {
		return nil, err
	}
	return &collectionOutput{Body: collection}, nil
}

// deleteCollection removes a collection and its saved entries. Only the owner
// may delete it.
func (s *server) deleteCollection(ctx context.Context, in *CollectionPath) (*struct{}, error) {
	return nil, s.catalog.DeleteCollection(ctx, in.CollectionID, viewerFrom(ctx))
}

// addCollectionEntry saves an entry into a collection. Re-adding an entry
// already present just moves it to the given position.
func (s *server) addCollectionEntry(ctx context.Context, in *addCollectionEntryInput) (*struct{}, error) {
	return nil, s.catalog.AddCollectionEntry(
		ctx, in.CollectionID, viewerFrom(ctx), in.Body.EntryID, in.Body.Position,
	)
}

// reorderCollectionEntries stores the owner's order of a collection's entries.
func (s *server) reorderCollectionEntries(ctx context.Context, in *reorderCollectionEntriesInput) (*struct{}, error) {
	return nil, s.catalog.ReorderCollectionEntries(ctx, in.CollectionID, viewerFrom(ctx), in.Body.EntryIDs)
}

// removeCollectionEntry removes an entry from a collection.
func (s *server) removeCollectionEntry(ctx context.Context, in *removeCollectionEntryInput) (*struct{}, error) {
	return nil, s.catalog.RemoveCollectionEntry(ctx, in.CollectionID, viewerFrom(ctx), in.EntryID)
}

// tagList is the curated, fixed set of discovery tags, grouped.
type tagList struct {
	Groups []catalog.TagGroup `json:"groups" doc:"Tag groups with their tags, both in display order."`
}

type tagListOutput struct {
	Body tagList
}

func (s *server) listTags(ctx context.Context, _ *struct{}) (*tagListOutput, error) {
	groups, err := s.catalog.ListTagGroups(ctx)
	if err != nil {
		return nil, err
	}
	return &tagListOutput{Body: tagList{Groups: groups}}, nil
}

func (s *server) registerCollections(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listEntryCollections", Method: http.MethodGet, Path: "/entries/{entryID}/collections",
		Tags:        []string{"Collections"},
		Summary:     "The caller's collections, flagged by whether they hold an entry",
		Description: "One request for the collection picker: every collection the caller owns, each with a contains flag for the entry.",
		Errors:      []int{http.StatusNotFound},
	}, accessRequired, limitRead), s.listEntryCollections)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "createCollection", Method: http.MethodPost, Path: "/collections",
		Tags:          []string{"Collections"},
		Summary:       "Create a collection",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxCollectionRequestBytes,
		Errors:        []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge},
	}, accessRequired, limitWrite), s.createCollection)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listMyCollections", Method: http.MethodGet, Path: "/collections",
		Tags:    []string{"Collections"},
		Summary: "The caller's collections",
	}, accessRequired, limitRead), s.listMyCollections)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getCollection", Method: http.MethodGet, Path: "/collections/{collectionID}",
		Tags:        []string{"Collections"},
		Summary:     "Read a collection with its entries",
		Description: "Public collections are readable by anyone; private collections are visible only to their owner.",
		Errors:      []int{http.StatusNotFound},
	}, accessOptional, limitRead), s.getCollection)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "updateCollection", Method: http.MethodPatch, Path: "/collections/{collectionID}",
		Tags:         []string{"Collections"},
		Summary:      "Update a collection",
		Description:  "Replaces the collection's name, description and visibility. Only the owner may update it.",
		MaxBodyBytes: maxCollectionRequestBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge},
	}, accessRequired, limitWrite), s.updateCollection)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "deleteCollection", Method: http.MethodDelete, Path: "/collections/{collectionID}",
		Tags:        []string{"Collections"},
		Summary:     "Delete a collection",
		Description: "Removes the collection and its saved entries. Only the owner may delete it.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound},
	}, accessRequired, limitWrite), s.deleteCollection)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "addCollectionEntry", Method: http.MethodPost, Path: "/collections/{collectionID}/entries",
		Tags:         []string{"Collections"},
		Summary:      "Save an entry into a collection",
		Description:  "Re-adding an entry already present moves it to the given position.",
		MaxBodyBytes: maxCollectionRequestBytes,
		Errors:       []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge},
	}, accessRequired, limitWrite), s.addCollectionEntry)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "reorderCollectionEntries", Method: http.MethodPut, Path: "/collections/{collectionID}/entries",
		Tags:         []string{"Collections"},
		Summary:      "Reorder a collection's entries",
		Description:  "Stores the owner's order of the collection's entries.",
		MaxBodyBytes: maxCollectionRequestBytes,
		Errors:       []int{http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge},
	}, accessRequired, limitWrite), s.reorderCollectionEntries)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "removeCollectionEntry", Method: http.MethodDelete, Path: "/collections/{collectionID}/entries/{entryID}",
		Tags:    []string{"Collections"},
		Summary: "Remove an entry from a collection",
		Errors:  []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}, accessRequired, limitWrite), s.removeCollectionEntry)
}

// registerTags registers this file's operations.
func (s *server) registerTags(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listTags", Method: http.MethodGet, Path: "/tags",
		Tags:        []string{readAPI, "Reference data"},
		Summary:     "The curated tag list",
		Description: "Tag groups with their tags, both in display order. Tags apply to entries.",
	}, accessPublic, limitRead), s.listTags)
}
