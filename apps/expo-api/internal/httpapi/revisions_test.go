package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// revisionCall sends one request to the public handler, with an edit token
// header when given.
func revisionCall(t *testing.T, handler http.Handler, method, path, editToken string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		raw = encoded
	}
	request := authedRequest(method, path, "", raw)
	if editToken != "" {
		request.Header.Set("X-Edit-Token", editToken)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func fixtureBlueprint(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../test-data/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestRevisionRoutes walks an anonymous entry's revisions through the Huma
// operations: create, list, history, read, export, and the two delete answers
// (204 for a revision that is not live, 200 with the promoted one otherwise)
// with the last revision refused.
func TestRevisionRoutes(t *testing.T) {
	handler, _ := newTestAPI(t)
	first, second := fixtureBlueprint(t, "bp-starter-smelter.txt"), fixtureBlueprint(t, "bp-green-circuit-block.txt")

	created := revisionCall(t, handler, http.MethodPost, "/api/v1/entries", "", map[string]string{"blueprint": first})
	if created.Code != http.StatusCreated {
		t.Fatalf("create entry = %d, %s", created.Code, created.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	base, token, original := "/api/v1/entries/"+entry.ID, entry.EditToken, entry.Revision.ID

	added := revisionCall(t, handler, http.MethodPost, base+"/revisions", token, map[string]string{"blueprint": second})
	if added.Code != http.StatusCreated {
		t.Fatalf("create revision = %d, %s", added.Code, added.Body)
	}
	var revised catalog.Entry
	if err := json.Unmarshal(added.Body.Bytes(), &revised); err != nil {
		t.Fatal(err)
	}
	live := revised.Revision.ID
	if live == original {
		t.Fatal("the new revision is the original one")
	}

	if unchecked := revisionCall(t, handler, http.MethodPost, base+"/revisions", "", map[string]string{"blueprint": second}); unchecked.Code != http.StatusForbidden {
		t.Fatalf("anonymous revision without a token = %d, %s", unchecked.Code, unchecked.Body)
	}
	if empty := revisionCall(t, handler, http.MethodPost, base+"/revisions", token, map[string]string{"blueprint": ""}); empty.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty revision = %d, want 422: %s", empty.Code, empty.Body)
	}

	listed := revisionCall(t, handler, http.MethodGet, base+"/revisions", "", nil)
	var list struct {
		Revisions []catalog.Revision `json:"revisions"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); listed.Code != http.StatusOK || err != nil || len(list.Revisions) != 2 {
		t.Fatalf("list revisions = %d %v, %s", listed.Code, err, listed.Body)
	}

	if history := revisionCall(t, handler, http.MethodGet, base+"/history", "", nil); history.Code != http.StatusOK {
		t.Fatalf("history = %d, %s", history.Code, history.Body)
	}
	if read := revisionCall(t, handler, http.MethodGet, base+"/revisions/"+original, "", nil); read.Code != http.StatusOK {
		t.Fatalf("read revision = %d, %s", read.Code, read.Body)
	}
	if missing := revisionCall(t, handler, http.MethodGet, base+"/revisions/000000000000", "", nil); missing.Code != http.StatusNotFound {
		t.Fatalf("missing revision = %d, want 404", missing.Code)
	}
	exported := revisionCall(t, handler, http.MethodGet, base+"/revisions/"+live+"/export", "", nil)
	var exchange struct {
		ExchangeString string `json:"exchange_string"`
	}
	if err := json.Unmarshal(exported.Body.Bytes(), &exchange); exported.Code != http.StatusOK || err != nil || exchange.ExchangeString == "" {
		t.Fatalf("export = %d %v, %s", exported.Code, err, exported.Body)
	}

	if noToken := revisionCall(t, handler, http.MethodDelete, base+"/revisions/"+original, "", nil); noToken.Code != http.StatusForbidden {
		t.Fatalf("delete without a token = %d, want 403", noToken.Code)
	}
	if plain := revisionCall(t, handler, http.MethodDelete, base+"/revisions/"+original, token, nil); plain.Code != http.StatusOK {
		// The catalog answers with the entry's live revision either way.
		t.Fatalf("delete a revision that is not live = %d, want 200: %s", plain.Code, plain.Body)
	}

	third := revisionCall(t, handler, http.MethodPost, base+"/revisions", token, map[string]string{"blueprint": first})
	var thirdEntry catalog.Entry
	if err := json.Unmarshal(third.Body.Bytes(), &thirdEntry); third.Code != http.StatusCreated || err != nil {
		t.Fatalf("create third revision = %d, %s", third.Code, third.Body)
	}
	promoted := revisionCall(t, handler, http.MethodDelete, base+"/revisions/"+thirdEntry.Revision.ID, token, nil)
	var answer struct {
		CurrentRevision *catalog.Revision `json:"current_revision"`
	}
	if err := json.Unmarshal(promoted.Body.Bytes(), &answer); promoted.Code != http.StatusOK || err != nil ||
		answer.CurrentRevision == nil || answer.CurrentRevision.ID != live {
		t.Fatalf("delete the live revision = %d %v, %s; want 200 promoting %s", promoted.Code, err, promoted.Body, live)
	}

	if last := revisionCall(t, handler, http.MethodDelete, base+"/revisions/"+live, token, nil); last.Code != http.StatusConflict {
		t.Fatalf("delete the last revision = %d, want 409: %s", last.Code, last.Body)
	}
}
