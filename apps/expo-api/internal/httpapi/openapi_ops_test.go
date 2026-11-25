package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Unknown query parameters and body fields fail validation with a
// validation_failed problem naming them, before the handler runs.
func TestOperationsRejectUnknownInput(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		request  *http.Request
		location string
	}{
		{"query parameter", httptest.NewRequest(http.MethodGet, "/api/v1/tags?bogus=1", nil), "query.bogus"},
		{"body field", jsonRequest(http.MethodPatch, "/api/v1/entries/abcdefghijkl", `{"title":"x","bogus":1}`), "body.bogus"},
	} {
		response := httptest.NewRecorder()
		serveRoutes(&server{}, response, test.request)
		var problem Problem
		if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
			t.Fatalf("%s: body %q: %v", test.name, response.Body, err)
		}
		if response.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
			len(problem.Errors) != 1 || problem.Errors[0].Location != test.location ||
			response.Header().Get("Content-Type") != problemContentType {
			t.Errorf("%s: %d %s", test.name, response.Code, response.Body)
		}
	}
}

func jsonRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

// The raw routes keep their open CORS: preflights and wrong methods are
// answered by the raw table, not the API's.
func TestRawRoutesDispatch(t *testing.T) {
	t.Parallel()
	handler := New(Dependencies{})
	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, httptest.NewRequest(http.MethodOptions, "/api/v1/blueprints/abcdefghijkl/raw", nil))
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight = %d %v", preflight.Code, preflight.Header())
	}
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/blueprints/abcdefghijkl/raw", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("POST = %d %v", post.Code, post.Header())
	}
}

type sliceHolder struct {
	Tags     []string            `json:"tags"`
	Optional []string            `json:"optional,omitempty"`
	Nested   *sliceHolder        `json:"nested,omitempty"`
	Items    []sliceHolder       `json:"items"`
	ByName   map[string][]string `json:"by_name"`
	Raw      json.RawMessage     `json:"raw,omitempty"`
}

// Responses answer nil slices as [] and nil maps as {} (the document
// declares both non-nullable) and leave the handler's value untouched.
func TestEmptySlices(t *testing.T) {
	t.Parallel()
	original := sliceHolder{
		Nested: &sliceHolder{},
		Items:  []sliceHolder{{Tags: []string{"a"}}},
		ByName: map[string][]string{"x": nil},
	}
	transformed, err := emptySlices(nil, "200", original)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(transformed)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"tags":[],"nested":{"tags":[],"items":[],"by_name":{}},"items":[{"tags":["a"],"items":[],"by_name":{}}],"by_name":{"x":[]}}`
	if string(body) != want {
		t.Errorf("body = %s\nwant %s", body, want)
	}
	if original.Tags != nil || original.Nested.Tags != nil || original.ByName["x"] != nil {
		t.Errorf("the original changed: %+v", original)
	}
}
