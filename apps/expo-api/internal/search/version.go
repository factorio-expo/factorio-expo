package search

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// projectionVersion is mixed into the index version. Bump it whenever the
// documents built for unchanged catalog data change (new derived values,
// different search_text, fixed facts) without a schema change; schema changes
// are picked up automatically from the schema definitions.
const projectionVersion = 1

// logicalSchemas lists the collections by their stable alias names.
var logicalSchemas = []collectionSchema{schemaBlueprints}

// collectionNames maps the stable logical collection names (also Typesense
// aliases) to this build's versioned physical collections. The client always
// reads and writes the physical names, so two API builds with different
// schemas never touch each other's collections.
type collectionNames struct {
	Version    string
	Blueprints string
}

// indexVersion fingerprints the collection schemas plus projectionVersion.
func indexVersion() string {
	encoded, err := json.Marshal(logicalSchemas)
	if err != nil {
		panic("encode search schemas: " + err.Error())
	}
	// The community names are part of the index: a changed list is a new
	// version, so the synonym set is reloaded with the rebuild.
	synonyms, err := json.Marshal(communitySynonyms())
	if err != nil {
		panic("encode community synonyms: " + err.Error())
	}
	sum := sha256.Sum256(append(append(encoded, synonyms...), []byte("projection:"+strconv.Itoa(projectionVersion))...))
	return hex.EncodeToString(sum[:])[:12]
}

func namesForVersion(version string) collectionNames {
	return collectionNames{
		Version:    version,
		Blueprints: "blueprints_" + version,
	}
}

func (n collectionNames) physical(logical string) string {
	switch logical {
	case "blueprints":
		return n.Blueprints
	}
	panic("unknown search collection " + logical)
}

// schemas returns the logical schemas renamed to their physical collections.
func (n collectionNames) schemas() []collectionSchema {
	out := make([]collectionSchema, 0, len(logicalSchemas))
	for _, schema := range logicalSchemas {
		out = append(out, collectionSchema{Name: n.physical(schema.Name), Fields: schema.Fields, SynonymSets: schema.SynonymSets})
	}
	return out
}

// isSearchCollection reports whether name is a collection this package owns:
// a legacy unversioned collection or any version of a logical collection.
func isSearchCollection(name string) bool {
	return slices.ContainsFunc(logicalSchemas, func(schema collectionSchema) bool {
		return name == schema.Name || strings.HasPrefix(name, schema.Name+"_")
	})
}
