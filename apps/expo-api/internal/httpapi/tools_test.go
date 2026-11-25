package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/ratelimit"
)

// The string tools touch no database, so these run without TEST_DATABASE_URL.
func newToolsAPI() http.Handler {
	return httpapi.New(httpapi.Dependencies{
		AllowedOrigin: "http://localhost:4321",
		Catalog:       catalog.New(nil, catalog.Options{}),
	})
}

func decodeBody[T any](t *testing.T, body []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return value
}

// Decoding and encoding round-trip: the encoded string decodes to the same JSON.
func TestDecodeEncodeRoundTrip(t *testing.T) {
	handler := newToolsAPI()
	decoded := postJSON(t, handler, "/api/v1/blueprints/decode", map[string]string{
		"blueprint": readFixture(t, "book-starter-kit.txt"),
	})
	if decoded.Code != http.StatusOK {
		t.Fatalf("decode = %d, %s", decoded.Code, decoded.Body)
	}
	payload := decodeBody[struct {
		Payload json.RawMessage `json:"payload"`
	}](t, decoded.Body.Bytes()).Payload
	if !strings.Contains(string(payload[:20]), "blueprint_book") {
		t.Fatalf("payload starts %q", payload[:20])
	}

	encoded := postJSON(t, handler, "/api/v1/blueprints/encode", map[string]any{"payload": payload})
	if encoded.Code != http.StatusOK {
		t.Fatalf("encode = %d, %s", encoded.Code, encoded.Body)
	}
	exchange := decodeBody[struct {
		Blueprint string `json:"blueprint"`
	}](t, encoded.Body.Bytes()).Blueprint
	again := postJSON(t, handler, "/api/v1/blueprints/decode", map[string]string{"blueprint": exchange})
	if again.Code != http.StatusOK {
		t.Fatalf("decode again = %d, %s", again.Code, again.Body)
	}
	if got := decodeBody[struct {
		Payload json.RawMessage `json:"payload"`
	}](t, again.Body.Bytes()).Payload; string(got) != string(payload) {
		t.Fatal("round trip changed the JSON")
	}
}

// Encode refuses JSON that is not a blueprint the catalog would accept.
func TestEncodeRejectsInvalidBlueprints(t *testing.T) {
	handler := newToolsAPI()
	for name, payload := range map[string]any{
		"not an envelope": map[string]any{"label": "x"},
		"unknown item":    map[string]any{"selection_tool": map[string]any{}},
		"not an object":   []int{1, 2},
	} {
		response := postJSON(t, handler, "/api/v1/blueprints/encode", map[string]any{"payload": payload})
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: encode = %d, %s", name, response.Code, response.Body)
		}
	}
	if response := postJSON(t, handler, "/api/v1/blueprints/encode", map[string]any{}); response.Code != http.StatusUnprocessableEntity {
		t.Errorf("missing payload: encode = %d", response.Code)
	}
}

func TestInspectBlueprint(t *testing.T) {
	handler := newToolsAPI()
	response := postJSON(t, handler, "/api/v1/blueprints/inspect", map[string]string{
		"blueprint": readFixture(t, "book-parametric-crafts.txt"),
	})
	if response.Code != http.StatusOK {
		t.Fatalf("inspect = %d, %s", response.Code, response.Body)
	}
	inspection := decodeBody[map[string]any](t, response.Body.Bytes())
	if inspection["kind"] != "book" || inspection["label"] != "Parametric crafts" {
		t.Fatalf("inspection = %v", inspection)
	}
	if inspection["blueprint_count"] != float64(10) {
		t.Fatalf("blueprint_count = %v", inspection["blueprint_count"])
	}
	// Lists are never null.
	for _, field := range []string{"suggested_tags", "entities", "recipes"} {
		if _, ok := inspection[field].([]any); !ok {
			t.Errorf("%s = %v, want an array", field, inspection[field])
		}
	}
	// The embedded mod table knows the book's Space Age content.
	mods, _ := inspection["mods"].(map[string]any)
	modsets, _ := mods["modsets"].([]any)
	if len(modsets) == 0 || modsets[0].(map[string]any)["name"] != "space-age" {
		t.Errorf("mods = %v, want space-age first", inspection["mods"])
	}
	for _, field := range []string{"unknown_entities", "unknown_recipes"} {
		if _, ok := mods[field].([]any); !ok {
			t.Errorf("mods.%s = %v, want an array", field, mods[field])
		}
	}
}

func TestInspectRejectsInvalidStrings(t *testing.T) {
	handler := newToolsAPI()
	for _, exchange := range []string{"not a blueprint", "0AAAA"} {
		response := postJSON(t, handler, "/api/v1/blueprints/inspect", map[string]string{"blueprint": exchange})
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("inspect %q = %d, %s", exchange, response.Code, response.Body)
		}
	}
	if response := postJSON(t, handler, "/api/v1/blueprints/decode", map[string]string{"blueprint": ""}); response.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty decode = %d", response.Code)
	}
	// An empty or missing exchange string fails the body schema.
	for _, body := range []map[string]string{{"blueprint": ""}, {}} {
		if response := postJSON(t, handler, "/api/v1/blueprints/inspect", body); response.Code != http.StatusUnprocessableEntity {
			t.Errorf("empty inspect %v = %d, %s", body, response.Code, response.Body)
		}
	}
}

// The tools share one budget of their own: 30 anonymous calls in a burst.
func TestToolsRateLimit(t *testing.T) {
	handler := httpapi.New(httpapi.Dependencies{
		AllowedOrigin: "http://localhost:4321",
		Catalog:       catalog.New(nil, catalog.Options{}),
		Limiter:       ratelimit.New(1000),
	})
	body := map[string]string{"blueprint": "not a blueprint"}
	for i := range 30 {
		path := "/api/v1/blueprints/inspect"
		if i%2 == 1 {
			path = "/api/v1/blueprints/decode"
		}
		if response := postJSON(t, handler, path, body); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("call %d = %d", i, response.Code)
		}
	}
	response := postJSON(t, handler, "/api/v1/blueprints/encode", map[string]any{"payload": map[string]any{}})
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("call 31 = %d, Retry-After %q", response.Code, response.Header().Get("Retry-After"))
	}
}

func TestListModsetsOffersTheEmbeddedModsets(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/modsets", nil)
	response := httptest.NewRecorder()
	newToolsAPI().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/modsets = %d, %s", response.Code, response.Body)
	}
	body := decodeBody[struct {
		Modsets []struct {
			Name  string `json:"name"`
			Title string `json:"title"`
		} `json:"modsets"`
	}](t, response.Body.Bytes())
	names := map[string]bool{}
	for _, modset := range body.Modsets {
		names[modset.Name] = modset.Title != ""
	}
	// Built-in mods are the filter's own Space Age option, never listed.
	if !names["aai-loaders"] || names["space-age"] || names["base"] {
		t.Fatalf("modsets = %+v", body.Modsets)
	}
}
