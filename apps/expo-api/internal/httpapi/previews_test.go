package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestQuickPreviewEndpointsAndRejection(t *testing.T) {
	truncateSocialFixtures(t)
	handler, dispatcher := newTestAPI(t)
	userToken, _ := registerTestUser(t, handler, "preview-user")
	read := func(name string) string {
		raw, err := os.ReadFile("../../../../test-data/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	if book := postJSON(t, handler, "/api/v1/previews", map[string]string{"blueprint": read("book-starter-kit.txt")}); book.Code != http.StatusUnprocessableEntity {
		t.Fatalf("book preview status = %d, %s", book.Code, book.Body)
	}
	created := postJSON(t, handler, "/api/v1/previews", map[string]string{"blueprint": read("bp-starter-smelter.txt")})
	if created.Code != http.StatusCreated {
		t.Fatalf("create preview status = %d, %s", created.Code, created.Body)
	}
	var preview catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.ExpiresAt == nil || preview.EditToken == "" {
		t.Fatalf("preview = %+v, want an expiry and an edit token", preview)
	}
	path := "/api/v1/entries/" + preview.ID

	send := func(method, url, jwt string, body []byte) *httptest.ResponseRecorder {
		request := authedRequest(method, url, jwt, body)
		request.Header.Set("X-Edit-Token", preview.EditToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	if patched := send(http.MethodPatch, path, "", []byte(`{"title":"x"}`)); patched.Code != http.StatusConflict {
		t.Fatalf("patch preview status = %d, %s", patched.Code, patched.Body)
	}
	publish := []byte(`{"title":"Published","visibility":"public"}`)
	if anonymous := send(http.MethodPost, path+"/publish", "", publish); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous publish status = %d, %s", anonymous.Code, anonymous.Body)
	}
	if early := send(http.MethodPost, path+"/publish", userToken, publish); early.Code != http.StatusConflict {
		t.Fatalf("unfinished publish status = %d, %s", early.Code, early.Body)
	}

	// The stub runner renders but fails the analysis for good: a preview is
	// the full package or nothing, so it is deleted (publishing a finished one
	// is covered in the catalog tests).
	pollUntilImported(t, dispatcher)
	if gone := send(http.MethodGet, path, "", nil); gone.Code != http.StatusNotFound {
		t.Fatalf("rejected preview status = %d, %s", gone.Code, gone.Body)
	}
}
