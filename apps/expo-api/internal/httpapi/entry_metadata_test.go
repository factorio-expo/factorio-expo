package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestEntryMetadataEndpoints(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	ownerToken, _ := registerTestUser(t, handler, "metadata-owner")
	otherToken, _ := registerTestUser(t, handler, "metadata-other")
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"blueprint": string(exchange), "title": "Initial", "description": "Original",
		"tags": []string{"starter-base"},
	})
	for _, owned := range []bool{false, true} {
		token := ""
		if owned {
			token = ownerToken
		}
		created := httptest.NewRecorder()
		handler.ServeHTTP(created, authedRequest(http.MethodPost, "/api/v1/entries", token, body))
		if created.Code != http.StatusCreated {
			t.Fatalf("create status = %d, %s", created.Code, created.Body)
		}
		var entry catalog.Entry
		if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/entries/" + entry.ID
		for _, test := range []struct {
			name      string
			jwt       string
			editToken string
			query     string
			allow     bool
		}{
			{"anonymous", "", "", "", false},
			{"other user", otherToken, "", "", false},
			{"wrong token", "", "wrong", "", false},
			{"owner", ownerToken, "", "", owned},
			{"header token", "", entry.EditToken, "", !owned},
			{"query token", "", "", "?token=" + entry.EditToken, !owned},
			{"query takes precedence", "", entry.EditToken, "?token=wrong", false},
		} {
			t.Run(test.name, func(t *testing.T) {
				permissions := httptest.NewRecorder()
				request := authedRequest(http.MethodGet, path+"/permissions"+test.query, test.jwt, nil)
				request.Header.Set("X-Edit-Token", test.editToken)
				handler.ServeHTTP(permissions, request)
				var payload map[string]bool
				if err := json.Unmarshal(permissions.Body.Bytes(), &payload); err != nil {
					t.Fatalf("permissions status=%d, body=%s", permissions.Code, permissions.Body)
				}
				if permissions.Code != http.StatusOK || !reflect.DeepEqual(payload, map[string]bool{"can_edit": test.allow}) {
					t.Fatalf("permissions = %d %+v, want %v", permissions.Code, payload, test.allow)
				}
				if permissions.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("permissions must not be cached")
				}
				request = authedRequest(http.MethodPatch, path+test.query, test.jwt, []byte(`{"title":"  Updated  "}`))
				request.Header.Set("X-Edit-Token", test.editToken)
				updated := httptest.NewRecorder()
				handler.ServeHTTP(updated, request)
				if !test.allow {
					if updated.Code != http.StatusForbidden {
						t.Fatalf("forbidden patch status = %d, %s", updated.Code, updated.Body)
					}
					return
				}
				var result catalog.Entry
				if err := json.Unmarshal(updated.Body.Bytes(), &result); err != nil {
					t.Fatalf("patch status = %d, %s", updated.Code, updated.Body)
				}
				if updated.Code != http.StatusOK || result.Title != "Updated" || result.Description != "Original" ||
					result.EditToken != "" || result.Revision.ID != entry.Revision.ID || !reflect.DeepEqual(result.Tags, entry.Tags) {
					t.Fatalf("metadata patch response = %d %+v", updated.Code, result)
				}
			})
		}
		// The catalog's rules answer 400; a body the schema refuses (an
		// unknown field, not an object) fails validation first: 422.
		for invalid, status := range map[string]int{
			`{"title":"  "}`: http.StatusBadRequest,
			`{"title":"` + strings.Repeat("x", 161) + `"}`:        http.StatusBadRequest,
			`{"description":"` + strings.Repeat("x", 5001) + `"}`: http.StatusBadRequest,
			`{"tags":["no-such-tag"]}`:                            http.StatusBadRequest,
			`{}`:                                                  http.StatusBadRequest,
			`{"blueprint":"other"}`:                               http.StatusUnprocessableEntity,
			`null`:                                                http.StatusUnprocessableEntity,
		} {
			request := authedRequest(http.MethodPatch, path, token, []byte(invalid))
			request.Header.Set("X-Edit-Token", entry.EditToken)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != status {
				t.Fatalf("invalid patch %s status = %d, want %d: %s", invalid, response.Code, status, response.Body)
			}
		}
		deleted := httptest.NewRecorder()
		request := authedRequest(http.MethodDelete, path, token, nil)
		request.Header.Set("X-Edit-Token", entry.EditToken)
		handler.ServeHTTP(deleted, request)
		if deleted.Code != http.StatusNoContent {
			t.Fatalf("delete = %d, %s", deleted.Code, deleted.Body)
		}
		for _, method := range []string{http.MethodGet, http.MethodPatch} {
			endpoint := path
			var body []byte
			if method == http.MethodGet {
				endpoint += "/permissions"
			} else {
				body = []byte(`{"title":"Updated"}`)
			}
			response := httptest.NewRecorder()
			request := authedRequest(method, endpoint, token, body)
			request.Header.Set("X-Edit-Token", entry.EditToken)
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("deleted entry %s = %d", method, response.Code)
			}
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authedRequest(http.MethodGet,
		"/api/v1/entries/000000000000/permissions", "", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing entry permissions = %d", response.Code)
	}
}
