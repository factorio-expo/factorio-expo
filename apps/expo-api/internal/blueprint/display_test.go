package blueprint

import (
	"encoding/json"
	"testing"
)

func TestDecodeLeafDisplayReadsBothFields(t *testing.T) {
	parameters := json.RawMessage(`[{"type":"id","id":"iron-plate"}]`)
	schedules := json.RawMessage(`[{"locomotives":[1],"schedule":{"records":[{"station":"A"}]}}]`)
	exchange, err := Encode(map[string]any{"blueprint": map[string]any{
		"parameters": parameters, "schedules": schedules,
	}})
	if err != nil {
		t.Fatal(err)
	}
	display, err := DecodeLeafDisplay(exchange, DefaultLimits().MaxDecodedBytes)
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, display.Parameters, string(parameters))
	assertJSON(t, display.Schedules, string(schedules))
}
