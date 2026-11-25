// Package staffops is the staff console's operations (docs/admin.md): the
// tag and reserved-name taxonomy, entries, users, bans, challenges, report
// queues and moderation, problem reports, global settings and the dry-run
// counts of the cleanups. The admin listener is its only caller. It lives
// beside the catalog, not in it: everything it changes goes through the
// catalog's small exported unit of work (catalog.Unit, Service.InChange,
// InTransaction, RecordAudit), so it cannot reach the catalog's internals,
// and each write records its audit row in the same transaction.
package staffops

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

// Service runs the staff operations over the catalog.
type Service struct {
	catalog *catalog.Service
	queries *db.Queries
	// deletion deletes accounts (the console's delete user).
	deletion *accountdeletion.Service
}

// New builds the staff operations over the catalog and the account deletion
// module.
func New(catalogService *catalog.Service, deletion *accountdeletion.Service) *Service {
	return &Service{catalog: catalogService, queries: catalogService.Queries(), deletion: deletion}
}

// parseID checks a short id taken from a request; a malformed one names
// nothing, so it is not found.
func parseID(value string) (string, error) {
	if !shortid.Valid(value) {
		return "", catalog.ErrNotFound
	}
	return value, nil
}

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func invalidStaffRequest(format string, args ...any) error {
	return audit.ErrInvalidRequest.Withf(format, args...)
}

// printable: valid UTF-8 without control characters.
func printable(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// problemReportListLimit bounds one console page of problem reports.
const problemReportListLimit = 200
