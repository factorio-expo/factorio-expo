package blueprint

import (
	"encoding/json"
	"testing"
)

func TestSchedules(t *testing.T) {
	schedules := `[{"locomotives":[1],"schedule":{"records":[{"station":"Iron","allows_unloading":false,"wait_conditions":[{"type":"time","compare_type":"and","ticks":600}]}],` +
		`"interrupts":[{"name":"Refuel","conditions":[{"type":"fuel_item_count_all","condition":{"first_signal":{"type":"item","name":"coal"},"comparator":"<","constant":5}}],"targets":[{"station":"Fuel"}]}]}}]`
	cases := map[string]struct {
		blueprint map[string]any
		want      string
	}{
		"present":    {map[string]any{"schedules": json.RawMessage(schedules)}, schedules},
		"empty":      {map[string]any{"schedules": []any{}}, "[]"},
		"absent":     {map[string]any{}, "null"},
		"not a list": {map[string]any{"schedules": map[string]any{}}, "null"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			exchange, err := Encode(map[string]any{"blueprint": test.blueprint})
			if err != nil {
				t.Fatal(err)
			}
			display, err := DecodeLeafDisplay(exchange, DefaultLimits().MaxDecodedBytes)
			if err != nil {
				t.Fatal(err)
			}
			assertJSON(t, display.Schedules, test.want)
		})
	}
}
