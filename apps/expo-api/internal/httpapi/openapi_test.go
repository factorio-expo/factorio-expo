package httpapi

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/search"
)

// The checked-in OpenAPI documents the frontends generate their API types
// from (and expo-web publishes, for the public one).
const (
	specPath      = "../../openapi/public.json"
	adminSpecPath = "../../openapi/admin.json"
)

// TestOpenAPISpecIsCurrent keeps the checked-in documents equal to the ones
// the operations generate. Regenerate them with
// `go run ./apps/expo-api/cmd/expo-api openapi > apps/expo-api/openapi/public.json`
// and `... openapi admin > apps/expo-api/openapi/admin.json`.
func TestOpenAPISpecIsCurrent(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path     string
		generate func() ([]byte, error)
		command  string
	}{
		{specPath, PublicSpec, "openapi > apps/expo-api/openapi/public.json"},
		{adminSpecPath, AdminSpec, "openapi admin > apps/expo-api/openapi/admin.json"},
	} {
		want, err := test.generate()
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(test.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s is stale; regenerate it with `go run ./apps/expo-api/cmd/expo-api %s`",
				test.path, test.command)
		}
	}
}

// TestSearchParametersMatchFilterTable keeps the documented search
// parameters (searchInput) equal to the ones search.ParseQuery reads.
func TestSearchParametersMatchFilterTable(t *testing.T) {
	t.Parallel()
	spec, err := PublicSpec()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(spec, &document); err != nil {
		t.Fatal(err)
	}
	var documented []string
	for _, parameter := range document.Paths["/api/v1/search"]["get"].Parameters {
		if parameter.In == "query" {
			documented = append(documented, parameter.Name)
		}
	}
	read := search.Parameters()
	slices.Sort(documented)
	slices.Sort(read)
	if !slices.Equal(documented, read) {
		t.Errorf("documented search parameters %v, search.ParseQuery reads %v", documented, read)
	}
}

// TestOperationsDeclarePathParameters catches an input struct whose path
// parameters Huma never sees (an embedded unexported struct is skipped):
// every {name} in a path must be a declared path parameter.
func TestOperationsDeclarePathParameters(t *testing.T) {
	t.Parallel()
	for name, generate := range map[string]func() ([]byte, error){"public": PublicSpec, "admin": AdminSpec} {
		spec, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		checkPathParameters(t, name, spec)
	}
}

func checkPathParameters(t *testing.T, name string, spec []byte) {
	t.Helper()
	var document struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(spec, &document); err != nil {
		t.Fatal(err)
	}
	for path, operations := range document.Paths {
		var want []string
		for segment := range strings.SplitSeq(path, "/") {
			if name, ok := strings.CutPrefix(segment, "{"); ok {
				want = append(want, strings.TrimSuffix(name, "}"))
			}
		}
		for method, operation := range operations {
			var declared []string
			for _, parameter := range operation.Parameters {
				if parameter.In == "path" {
					declared = append(declared, parameter.Name)
				}
			}
			slices.Sort(declared)
			slices.Sort(want)
			if !slices.Equal(declared, want) {
				t.Errorf("%s: %s %s declares path parameters %v, want %v", name, method, path, declared, want)
			}
		}
	}
}

// thirdPartySchemas are the WebAuthn types the passkey routes pass through
// from go-webauthn: their fields are the W3C specification's.
var thirdPartySchemas = map[string]bool{
	"AuthenticationExtensions": true, "AuthenticatorSelection": true, "HMACGetSecretInputs": true,
	"LargeBlobInputs": true, "PRFInputs": true, "PRFValues": true, "UserEntity": true,
	"RelyingPartyEntity": true, "CredentialParameter": true, "CredentialDescriptor": true,
}

// TestPublicSpecDocumentsEveryField keeps the public document explaining
// every field: a new response field needs its `doc` tag.
func TestPublicSpecDocumentsEveryField(t *testing.T) {
	t.Parallel()
	raw, err := PublicSpec()
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	for name, schema := range spec.Components.Schemas {
		if thirdPartySchemas[name] || strings.HasPrefix(name, "PublicKeyCredential") {
			continue
		}
		for property, definition := range schema.Properties {
			if definition.Description == "" {
				t.Errorf("%s.%s has no description: add a doc tag to the Go field", name, property)
			}
		}
	}
}
