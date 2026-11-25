package settings

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	number := Definition{Key: "N", Kind: KindInt, Min: 1, Max: 10}
	text := Definition{Key: "T", Kind: KindString, MaxRunes: 5}
	flag := Definition{Key: "B", Kind: KindBool}
	tests := []struct {
		name string
		def  Definition
		raw  string
		want string
		bad  bool
	}{
		{"bool true", flag, "true", "true", false},
		{"bool word", flag, "yes", "", true},
		{"bool empty", flag, "", "", true},
		{"int", number, "07", "7", false},
		{"int low", number, "0", "", true},
		{"int high", number, "11", "", true},
		{"int text", number, "x", "", true},
		{"string trimmed", text, "  hi  ", "hi", false},
		{"string empty", text, "", "", false},
		{"string long", text, "abcdef", "", true},
		{"string runes", text, "ééééé", "ééééé", false},
		{"string newline", text, "a\nb", "", true},
	}
	for _, test := range tests {
		got, err := test.def.Normalize(test.raw)
		if test.bad {
			if !errors.Is(err, ErrInvalidValue) {
				t.Errorf("%s: error = %v, want ErrInvalidValue", test.name, err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("%s: got %q, %v; want %q", test.name, got, err, test.want)
		}
	}
}

func TestDefinitionsAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, definition := range Definitions() {
		if seen[definition.Key] {
			t.Errorf("duplicate key %s", definition.Key)
		}
		seen[definition.Key] = true
		if definition.Description == "" {
			t.Errorf("%s has no description", definition.Key)
		}
		if _, err := definition.Normalize(definition.Default); err != nil {
			t.Errorf("%s default %q does not fit: %v", definition.Key, definition.Default, err)
		}
	}
}

func TestMergeSkipsUnknownAndInvalid(t *testing.T) {
	values, skipped := Merge(map[string]string{
		AllowUploads:  "false",
		AllowComments: "maybe",
		"GONE":        "1",
		SiteBanner:    "  down for maintenance ",
	})
	if values.Bool(AllowUploads) {
		t.Error("ALLOW_UPLOADS stored false but reads true")
	}
	if !values.Bool(AllowComments) {
		t.Error("an invalid stored value must fall back to the default")
	}
	if values.String(SiteBanner) != "down for maintenance" {
		t.Errorf("banner = %q", values.String(SiteBanner))
	}
	if len(skipped) != 2 || skipped[0] != AllowComments || skipped[1] != "GONE" {
		t.Errorf("skipped = %v", skipped)
	}
	if !values.Bool(AllowSignups) {
		t.Error("an unset key must read its default")
	}
}

func TestCacheReusesThenReloads(t *testing.T) {
	calls := 0
	stored := map[string]string{AllowUploads: "false"}
	now := time.Unix(0, 0)
	cache := NewCache(func(context.Context) (map[string]string, error) {
		calls++
		return stored, nil
	}, time.Second)
	cache.now = func() time.Time { return now }

	if cache.Get(t.Context()).Bool(AllowUploads) {
		t.Fatal("first read ignored the stored row")
	}
	stored = map[string]string{AllowUploads: "true"}
	cache.Get(t.Context())
	if calls != 1 {
		t.Fatalf("loads within ttl = %d, want 1", calls)
	}
	now = now.Add(2 * time.Second)
	if !cache.Get(t.Context()).Bool(AllowUploads) || calls != 2 {
		t.Fatalf("after ttl: loads = %d", calls)
	}
	stored = map[string]string{AllowUploads: "false"}
	cache.Invalidate()
	if cache.Get(t.Context()).Bool(AllowUploads) || calls != 3 {
		t.Fatalf("after Invalidate: loads = %d", calls)
	}
}

func TestCacheKeepsLastValuesWhenTheTableCannotBeRead(t *testing.T) {
	fail := false
	cache := NewCache(func(context.Context) (map[string]string, error) {
		if fail {
			return nil, errors.New("down")
		}
		return map[string]string{AllowSignups: "false"}, nil
	}, time.Nanosecond)
	cache.Get(t.Context())
	time.Sleep(time.Millisecond)
	fail = true
	if cache.Get(t.Context()).Bool(AllowSignups) {
		t.Fatal("a failed read must keep the last values, not reopen the switch")
	}
}
