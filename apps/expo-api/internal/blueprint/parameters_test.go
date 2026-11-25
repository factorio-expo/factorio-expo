package blueprint

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParameters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"absent", `{"blueprint":{"item":"blueprint"}}`, ""},
		{"empty", `{"blueprint":{"parameters":[]}}`, `[]`},
		{"null", `{"blueprint":{"parameters":null}}`, `null`},
		{
			"signals and numbers in source order",
			`{"blueprint":{"parameters":[
				{"type":"id","id":"parameter-0","name":"Product","parameter":true},
				{"type":"id","id":"parameter-9","name":"Ingredient","ingredient-of":"parameter-0","parameter":false,"quality-condition":{"quality":"rare","comparator":">="}},
				{"type":"number","number":"9007199254740993","name":"Constant","not-parametrised":true},
				{"type":"number","number":"100","name":"Amount","variable":"amount"},
				{"type":"number","number":"200","name":"Calculated","variable":"result","formula":"amount * 2 + p0_i1","dependent":true}
			]}}`,
			`[
				{"type":"id","id":"parameter-0","name":"Product","parameter":true},
				{"type":"id","id":"parameter-9","name":"Ingredient","ingredient-of":"parameter-0","parameter":false,"quality-condition":{"quality":"rare","comparator":">="}},
				{"type":"number","number":"9007199254740993","name":"Constant","not-parametrised":true},
				{"type":"number","number":"100","name":"Amount","variable":"amount"},
				{"type":"number","number":"200","name":"Calculated","variable":"result","formula":"amount * 2 + p0_i1","dependent":true}
			]`,
		},
		{
			"no parameter validation; unknown fields dropped, misfits skipped",
			`{"blueprint":{"parameters":[{"type":"number","number":"invalid","formula":"unknown(","extra":true},{"type":"future"},null,{"type":"id","name":5}]}}`,
			`[{"type":"number","number":"invalid","formula":"unknown("},{"type":"future"}]`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exchange, err := Encode(json.RawMessage(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			display, err := DecodeLeafDisplay(exchange, DefaultLimits().MaxDecodedBytes)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" || test.want == "null" {
				if display.Parameters != nil {
					t.Fatalf("parameters = %+v, want none", display.Parameters)
				}
				return
			}
			assertJSON(t, display.Parameters, test.want)
		})
	}
}

func TestParametersPreservedInBookLeaf(t *testing.T) {
	t.Parallel()
	exchange, err := Encode(json.RawMessage(`{"blueprint_book":{"blueprints":[
		{"index":0,"blueprint_book":{"blueprints":[
			{"index":0,"blueprint":{"parameters":[{"type":"id","id":"parameter-0"}]}}
		]}},
		{"index":1,"blueprint":{}}
	]}}`))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := Parse(exchange, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	leaves := collectLeaves(tree)
	if len(leaves) != 2 {
		t.Fatalf("leaves = %d, want 2", len(leaves))
	}
	for index, want := range []string{`[{"type":"id","id":"parameter-0"}]`, "null"} {
		display, err := DecodeLeafDisplay(leaves[index].ExchangeString, DefaultLimits().MaxDecodedBytes)
		if err != nil {
			t.Fatal(err)
		}
		assertJSON(t, display.Parameters, want)
	}
}

func TestParametersDecodeErrors(t *testing.T) {
	t.Parallel()
	if _, err := DecodeLeafDisplay("invalid", DefaultLimits().MaxDecodedBytes); !errors.Is(err, ErrInvalidExchangeString) {
		t.Fatalf("error = %v, want invalid exchange", err)
	}

	exchange, err := Encode(json.RawMessage(`{"blueprint":{"parameters":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeLeafDisplay(exchange, 1); !errors.Is(err, ErrInvalidExchangeString) {
		t.Fatalf("error = %v, want size limit error", err)
	}
}

func TestParametersRelationshipFixture(t *testing.T) {
	t.Parallel()
	exchange, err := os.ReadFile("../../../../test-data/bp-parametric-assembler-row.txt")
	if err != nil {
		t.Fatal(err)
	}
	display, err := DecodeLeafDisplay(strings.TrimSpace(string(exchange)), DefaultLimits().MaxDecodedBytes)
	if err != nil {
		t.Fatal(err)
	}
	parameters := display.Parameters
	if len(parameters) != 6 {
		t.Fatalf("parameters = %d, want 6", len(parameters))
	}
	for index, got := range []string{
		parameters[1].IngredientOf, parameters[2].ItemIngredientOf, parameters[3].FluidIngredientOf,
		parameters[4].ProductOf, parameters[5].ItemProductOf,
	} {
		if got != "parameter-0" {
			t.Fatalf("parameter %d: relation = %q", index+1, got)
		}
	}
}

// assertJSON compares value's JSON with want, ignoring key order.
func assertJSON(t *testing.T, value any, want string) {
	t.Helper()
	got, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("got %s, want %s", got, want)
	}
}
