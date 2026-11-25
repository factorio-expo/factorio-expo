package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The API is its Huma operations: no route may be registered on a mux by
// hand, anywhere in the package. raw.go's path-only table (the dispatcher's
// list of raw paths, answered 404 itself) is the one place a mux gets
// patterns, and none of them is a route.
var muxRoute = regexp.MustCompile(`\.Handle(Func)?\(`)

func TestRoutesAreHumaOperations(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "raw.go" {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(source), "\n") {
			if muxRoute.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d registers a route by hand: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// registerOperations only calls the files' register functions: each file
// registers its own operations (huma.Register lives next to its handler).
func TestRegisterOperationsOnlyCallsRegisterFunctions(t *testing.T) {
	t.Parallel()
	call := regexp.MustCompile(`^\s*s\.register\w+\(api\)$`)
	for _, file := range []string{"routes.go", "admin_operations.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		start := strings.Index(text, "registerOperations(api huma.API) {")
		if start < 0 {
			t.Fatalf("%s has no registerOperations", file)
		}
		body, _, _ := strings.Cut(text[start:], "\n}\n")
		for _, line := range strings.Split(body, "\n")[1:] {
			if !call.MatchString(line) {
				t.Errorf("%s registerOperations has %q: move it into a register function of its own file", file, strings.TrimSpace(line))
			}
		}
	}
}

// schemaNode is the part of a schema the guard looks at.
type schemaNode struct {
	Ref                  string          `json:"$ref"`
	Type                 json.RawMessage `json:"type"`
	Properties           map[string]any  `json:"properties"`
	AdditionalProperties json.RawMessage `json:"additionalProperties"`
	Items                json.RawMessage `json:"items"`
	OneOf                []any           `json:"oneOf"`
	AnyOf                []any           `json:"anyOf"`
	Enum                 []any           `json:"enum"`
}

// typed says a schema describes something: a reference, a scalar, an array,
// an object with properties or a declared value type.
func (n schemaNode) typed() bool {
	if n.Ref != "" || len(n.Items) > 0 || len(n.OneOf) > 0 || len(n.AnyOf) > 0 || len(n.Enum) > 0 {
		return true
	}
	if len(n.Properties) > 0 || len(n.AdditionalProperties) > 0 && string(n.AdditionalProperties) != "true" {
		return true
	}
	return len(n.Type) > 0 && string(n.Type) != `"object"`
}

// Every operation of both documents is named and described, and answers
// with a typed body, a declared stream or an explicit empty success.
func TestOperationsAreDocumented(t *testing.T) {
	t.Parallel()
	for name, generate := range map[string]func() ([]byte, error){"public": PublicSpec, "admin": AdminSpec} {
		spec, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Paths map[string]map[string]struct {
				OperationID string   `json:"operationId"`
				Summary     string   `json:"summary"`
				Tags        []string `json:"tags"`
				Responses   map[string]struct {
					Content map[string]struct {
						Schema schemaNode `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
			} `json:"paths"`
		}
		if err := json.Unmarshal(spec, &document); err != nil {
			t.Fatal(err)
		}
		for path, operations := range document.Paths {
			for method, op := range operations {
				where := name + ": " + strings.ToUpper(method) + " " + path
				if op.OperationID == "" || op.Summary == "" || len(op.Tags) == 0 {
					t.Errorf("%s needs an operationId, a summary and tags", where)
				}
				success := false
				for status, response := range op.Responses {
					if status[0] != '2' && status[0] != '3' {
						continue
					}
					if status != "304" {
						success = success || len(response.Content) == 0 && (status == "204" || status[0] == '3')
					}
					for contentType, media := range response.Content {
						if !media.Schema.typed() {
							t.Errorf("%s: %s %s has an untyped schema", where, status, contentType)
						}
						success = success || media.Schema.typed()
					}
				}
				if !success {
					t.Errorf("%s has no typed success response", where)
				}
			}
		}
	}
}
