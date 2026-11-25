package catalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

func TestMetadataAuthorization(t *testing.T) {
	owner := shortid.New()
	tests := []struct {
		name  string
		entry db.Entry
		auth  Authorization
		allow bool
	}{
		{"owner", db.Entry{OwnerUserID: pgText(owner), Visibility: "public"}, Authorization{UserID: owner}, true},
		{"other user", db.Entry{OwnerUserID: pgText(owner), Visibility: "public"}, Authorization{UserID: shortid.New()}, false},
		{"no credentials", db.Entry{EditTokenHash: pgText(hashEditToken("secret")), Visibility: "public"}, Authorization{}, false},
		{"anonymous token", db.Entry{EditTokenHash: pgText(hashEditToken("secret")), Visibility: "public"}, Authorization{EditToken: "secret"}, true},
		{"wrong token", db.Entry{EditTokenHash: pgText(hashEditToken("secret")), Visibility: "public"}, Authorization{EditToken: "wrong"}, false},
		{"owner of private entry", db.Entry{OwnerUserID: pgText(owner), Visibility: "private"}, Authorization{UserID: owner}, true},
		{"other user of private entry", db.Entry{OwnerUserID: pgText(owner), Visibility: "private"}, Authorization{UserID: shortid.New()}, false},
		{"token cannot edit owned entry", db.Entry{OwnerUserID: pgText(owner), EditTokenHash: pgText(hashEditToken("secret")), Visibility: "public"}, Authorization{EditToken: "secret"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := authorize(test.entry, test.auth); (got == nil) != test.allow {
				t.Fatalf("authorize = %v, allow = %v", got, test.allow)
			}
		})
	}
}

func TestNormalizedMetadataUpdate(t *testing.T) {
	text := func(value string) *string { return &value }
	tests := []struct {
		name   string
		update EntryMetadataUpdate
		title  pgtype.Text
		desc   pgtype.Text
		bad    bool
	}{
		{"title only", EntryMetadataUpdate{Title: text("  新 title \n")}, pgText("新 title"), pgtype.Text{}, false},
		{"markdown source", EntryMetadataUpdate{Description: text(" \n**bold** [link](https://example.com)\n ")}, pgtype.Text{}, pgText("**bold** [link](https://example.com)"), false},
		{"clear description", EntryMetadataUpdate{Description: text(" \n ")}, pgtype.Text{}, pgtype.Text{String: "", Valid: true}, false},
		{"blank title", EntryMetadataUpdate{Title: text(" \n ")}, pgtype.Text{}, pgtype.Text{}, true},
		{"long title", EntryMetadataUpdate{Title: text(strings.Repeat("界", 161))}, pgtype.Text{}, pgtype.Text{}, true},
		{"long description", EntryMetadataUpdate{Description: text(strings.Repeat("界", 5001))}, pgtype.Text{}, pgtype.Text{}, true},
		{"limits inclusive", EntryMetadataUpdate{Title: text(strings.Repeat("界", 160)), Description: text(strings.Repeat("界", 5000))}, pgText(strings.Repeat("界", 160)), pgText(strings.Repeat("界", 5000)), false},
		{"tags only", EntryMetadataUpdate{Tags: &[]string{}}, pgtype.Text{}, pgtype.Text{}, false},
		{"visibility only", EntryMetadataUpdate{Visibility: func() *Visibility { v := VisibilityUnlisted; return &v }()}, pgtype.Text{}, pgtype.Text{}, false},
		{"no fields", EntryMetadataUpdate{}, pgtype.Text{}, pgtype.Text{}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			title, desc, err := normalizedMetadataUpdate(test.update)
			if test.bad {
				if !errors.Is(err, ErrInvalidEntryMetadata) {
					t.Fatalf("error = %v, want invalid metadata", err)
				}
				return
			}
			if err != nil || title != test.title || desc != test.desc {
				t.Fatalf("normalized metadata = (%+v, %+v, %v)", title, desc, err)
			}
		})
	}
}
