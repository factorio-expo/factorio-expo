package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestLeafDetailParameters(t *testing.T) {
	handler, _ := newTestAPI(t)
	parameters := json.RawMessage(`[
		{"type":"id","id":"parameter-0","name":"Product"},
		{"type":"id","id":"parameter-1","ingredient-of":"parameter-0","parameter":false},
		{"type":"number","number":"100","variable":"amount"},
		{"type":"number","number":"200","formula":"amount * 2","dependent":true}
	]`)
	exchange, err := blueprint.Encode(map[string]any{
		"blueprint_book": map[string]any{
			"blueprints": []any{
				map[string]any{"index": 0, "blueprint": map[string]any{"parameters": parameters}},
				map[string]any{"index": 1, "blueprint": map[string]any{}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	created := postJSON(t, handler, "/api/v1/entries", map[string]string{
		"blueprint": exchange, "title": "Parameter display",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	leaves := entry.Revision.Root.Leaves()
	if len(leaves) != 2 {
		t.Fatalf("leaves = %d, want 2", len(leaves))
	}
	var want []blueprint.Parameter
	if err := json.Unmarshal(parameters, &want); err != nil {
		t.Fatal(err)
	}
	for index, leaf := range leaves {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
			"/api/v1/entries/"+entry.ID+"/blueprints/"+leaf.ID, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("detail status = %d, body = %s", recorder.Code, recorder.Body)
		}
		var detail catalog.LeafDetail
		if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if index == 0 && !reflect.DeepEqual(detail.Parameters, want) {
			t.Fatalf("parameters = %+v, want %+v", detail.Parameters, want)
		}
		if index == 1 && bytes.Contains(recorder.Body.Bytes(), []byte(`"parameters"`)) {
			t.Fatal("non-parametrized leaf should omit parameters")
		}
	}
	if bytes.Contains(created.Body.Bytes(), []byte(`"parameters"`)) {
		t.Fatal("tree response should not contain parameters")
	}
}
