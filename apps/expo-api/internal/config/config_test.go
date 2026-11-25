package config

import (
	"maps"
	"strings"
	"testing"
	"time"
)

// Two distinct secrets of the minimum length.
const (
	secretA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secretB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// env is a minimal valid environment plus overrides; an empty override value
// unsets the name.
func env(overrides map[string]string) func(string) string {
	values := map[string]string{
		"DATABASE_URL":       "postgres://x",
		"API_JWT_SECRET":     secretA,
		"API_IP_HASH_SECRET": secretB,
		"TYPESENSE_API_KEY":  "a-random-key",
	}
	maps.Copy(values, overrides)
	return func(key string) string { return values[key] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.SiteURL != "http://localhost:3000" || len(cfg.Origins) != 2 ||
		cfg.PollInterval != 3*time.Second || cfg.QuickPreviewTTL != 24*time.Hour ||
		cfg.StorageMaxBytes != 150<<30 || cfg.StorageMinFreeBytes != 20<<30 ||
		cfg.ChallengeScoresTTL != 10*time.Second ||
		!cfg.CookieSecure || !cfg.RateLimit || cfg.AdminAddr != "" || cfg.RenderQueue.Window < 1 {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestLoadRules(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string // error substring; "" means the load succeeds
	}{
		{"valid admin listener", map[string]string{
			"API_ADMIN_ADDR": ":9090", "API_ADMIN_TOKEN": strings.Repeat("t", 32),
		}, ""},
		{"jwt secret too short", map[string]string{"API_JWT_SECRET": "short"}, "API_JWT_SECRET must be at least"},
		{"placeholder secret", map[string]string{"API_JWT_SECRET": strings.Repeat("change-me", 5)}, "placeholder"},
		{"previous secret equals current", map[string]string{"API_JWT_SECRET_PREVIOUS": secretA}, "must differ from API_JWT_SECRET"},
		{"previous secret too short", map[string]string{"API_JWT_SECRET_PREVIOUS": "short"}, "API_JWT_SECRET_PREVIOUS must be at least"},
		{"rotation with a distinct previous secret", map[string]string{"API_JWT_SECRET_PREVIOUS": strings.Repeat("c", 36)}, ""},
		{"jwt secret equals ip hash secret", map[string]string{"API_IP_HASH_SECRET": secretA}, "must differ"},
		{"admin listener without a token", map[string]string{"API_ADMIN_ADDR": ":9090"}, "API_ADMIN_TOKEN"},
		{"admin listener on the public address", map[string]string{
			"API_ADMIN_ADDR": ":8080", "API_ADMIN_TOKEN": strings.Repeat("t", 32),
		}, "must differ from API_HTTP_ADDR"},
		{"ip ban above a day", map[string]string{"API_IP_BAN_DURATION": "25h"}, "API_IP_BAN_DURATION"},
		{"ip ban of a day", map[string]string{"API_IP_BAN_DURATION": "24h"}, ""},
		{"ip ban not positive", map[string]string{"API_IP_BAN_DURATION": "0s"}, "API_IP_BAN_DURATION"},
		{"challenge board cache off", map[string]string{"API_CHALLENGE_SCORES_TTL": "0s"}, ""},
		{"challenge board cache negative", map[string]string{"API_CHALLENGE_SCORES_TTL": "-1s"}, "API_CHALLENGE_SCORES_TTL"},
		{"duration not parsable", map[string]string{"API_PREVIEW_POLL_INTERVAL": "soon"}, "API_PREVIEW_POLL_INTERVAL is not a duration"},
		{"view flush not positive", map[string]string{"API_VIEW_FLUSH_INTERVAL": "0s"}, "API_VIEW_FLUSH_INTERVAL"},
		{"negative encoders", map[string]string{"API_PREVIEW_ENCODERS": "-1"}, "API_PREVIEW_ENCODERS"},
		{"tag score below one", map[string]string{"API_TAG_SUGGESTION_MIN_SCORE": "0"}, "API_TAG_SUGGESTION_MIN_SCORE"},
		{"byte size", map[string]string{"API_STORAGE_MAX_BYTES": "lots"}, "API_STORAGE_MAX_BYTES is not a byte size"},
		{"byte size with a unit", map[string]string{"API_STORAGE_MAX_BYTES": "20 GB"}, ""},
		{"trusted proxies", map[string]string{"API_TRUSTED_PROXIES": "not-a-cidr"}, "API_TRUSTED_PROXIES"},
		{"public url on a real site", map[string]string{
			"API_ALLOWED_ORIGIN": "https://example.com",
		}, "API_PUBLIC_URL"},
		{"production origins", map[string]string{
			"API_ALLOWED_ORIGIN": "https://example.com", "API_PUBLIC_URL": "https://api.example.com",
		}, ""},
		{"no origin", map[string]string{"API_ALLOWED_ORIGIN": " , "}, "API_ALLOWED_ORIGIN"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(env(test.env))
			switch {
			case test.want == "" && err != nil:
				t.Fatalf("Load = %v, want success", err)
			case test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)):
				t.Fatalf("Load error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadRequiresDatabaseAndTypesense(t *testing.T) {
	for _, name := range []string{"DATABASE_URL", "TYPESENSE_API_KEY"} {
		lookup := env(nil)
		_, err := Load(func(key string) string {
			if key == name {
				return ""
			}
			return lookup(key)
		})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: error = %v", name, err)
		}
	}
}

func TestLoadRenderQueueOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"API_RENDER_WINDOW": "7", "API_RENDER_LEAF_GAP": "1500ms", "API_RENDER_DELAY_REPAIR": "0s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RenderQueue.Window != 7 || cfg.RenderQueue.LeafGap != 1500*time.Millisecond || cfg.RenderQueue.DelayRepair != 0 {
		t.Fatalf("render queue = %+v", cfg.RenderQueue)
	}
	for name, value := range map[string]string{
		"API_RENDER_WINDOW": "0", "API_RENDER_CAP_ANONYMOUS": "x",
		"API_RENDER_DELAY_SIGNED_IN": "-1s", "API_RENDER_LEAF_GAP": "slow",
	} {
		if _, err := Load(env(map[string]string{name: value})); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%s: error = %v", name, value, err)
		}
	}
}
