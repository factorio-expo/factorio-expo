package catalog

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeStatus(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		ok             bool
	}{
		{"trimmed", "  Building on [planet=gleba]  ", "Building on [planet=gleba]", true},
		{"empty clears", "   ", "", true},
		{"140 characters", strings.Repeat("é", MaxStatusLength), strings.Repeat("é", MaxStatusLength), true},
		{"too long", strings.Repeat("a", MaxStatusLength+1), "", false},
		{"two lines", "one\ntwo", "", false},
		{"control character", "tab\there", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeStatus(tc.in)
			if tc.ok != (err == nil) {
				t.Fatalf("normalizeStatus(%q) error = %v, want ok %v", tc.in, err, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("error %v is not ErrInvalidProfile", err)
			}
			if got != tc.want {
				t.Fatalf("normalizeStatus(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeLinks(t *testing.T) {
	links, err := normalizeLinks([]SocialLinkInput{
		{Platform: "x", Handle: "@expo_fan"},
		{Platform: "youtube", Handle: " @ExampleHandle "},
		{Platform: "reddit", Handle: "u/some-one"},
		{Platform: "bluesky", Handle: "@someone.bsky.social"},
		{Platform: "steam", Handle: ""}, // empty: no link
	})
	if err != nil {
		t.Fatalf("normalizeLinks: %v", err)
	}
	want := []SocialLink{
		{Platform: "youtube", Handle: "ExampleHandle", URL: "https://www.youtube.com/@ExampleHandle"},
		{Platform: "reddit", Handle: "some-one", URL: "https://www.reddit.com/user/some-one"},
		{Platform: "bluesky", Handle: "someone.bsky.social", URL: "https://bsky.app/profile/someone.bsky.social"},
		{Platform: "x", Handle: "expo_fan", URL: "https://x.com/expo_fan"},
	}
	if len(links) != len(want) {
		t.Fatalf("links = %+v, want %+v", links, want)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Fatalf("links[%d] = %+v, want %+v", i, links[i], want[i])
		}
	}
}

func TestNormalizeLinksRefuses(t *testing.T) {
	for name, inputs := range map[string][]SocialLinkInput{
		"unknown site":   {{Platform: "mastodon", Handle: "someone"}},
		"a URL":          {{Platform: "github", Handle: "https://github.com/someone"}},
		"a path":         {{Platform: "twitch", Handle: "someone/videos"}},
		"twice":          {{Platform: "github", Handle: "one"}, {Platform: "github", Handle: "two"}},
		"bad github":     {{Platform: "github", Handle: "-leading-dash"}},
		"long x":         {{Platform: "x", Handle: "sixteen_chars_xx"}},
		"bluesky no dot": {{Platform: "bluesky", Handle: "someone"}},
		"query string":   {{Platform: "steam", Handle: "name?x=1"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeLinks(inputs); !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("normalizeLinks(%+v) error = %v, want ErrInvalidProfile", inputs, err)
			}
		})
	}
}
