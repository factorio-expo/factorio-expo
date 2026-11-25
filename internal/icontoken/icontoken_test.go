package icontoken

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

type fixture struct {
	IconTypes []string `json:"iconTypes"`
	Qualities []string `json:"qualities"`
	Parse     []struct {
		Token string `json:"token"`
		Want  *Token `json:"want"`
	} `json:"parse"`
	Format []struct {
		Token      Token  `json:"token"`
		KeepNormal bool   `json:"keepNormal"`
		Want       string `json:"want"`
	} `json:"format"`
	Scan []struct {
		Text string    `json:"text"`
		Want []Scanned `json:"want"`
	} `json:"scan"`
	Strip []struct {
		Text string `json:"text"`
		Want string `json:"want"`
	} `json:"strip"`
}

func load(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile("../../test-data/icon-tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFixtureLists(t *testing.T) {
	f := load(t)
	got, want := IconTypes(), append([]string(nil), f.IconTypes...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("icon types = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(Qualities, f.Qualities) {
		t.Errorf("qualities = %v, want %v", Qualities, f.Qualities)
	}
}

func TestParse(t *testing.T) {
	for _, c := range load(t).Parse {
		got, ok := Parse(c.Token)
		if c.Want == nil {
			if ok {
				t.Errorf("Parse(%q) = %v, want no token", c.Token, got)
			}
			continue
		}
		if !ok || got != *c.Want {
			t.Errorf("Parse(%q) = %v, %v; want %v", c.Token, got, ok, *c.Want)
		}
	}
}

func TestFormat(t *testing.T) {
	for _, c := range load(t).Format {
		if got := Format(c.Token, c.KeepNormal); got != c.Want {
			t.Errorf("Format(%v, %v) = %q, want %q", c.Token, c.KeepNormal, got, c.Want)
		}
	}
}

func TestScan(t *testing.T) {
	for _, c := range load(t).Scan {
		if got := Scan(c.Text); !reflect.DeepEqual(got, c.Want) {
			t.Errorf("Scan(%q) = %v, want %v", c.Text, got, c.Want)
		}
	}
}

func TestStrip(t *testing.T) {
	for _, c := range load(t).Strip {
		if got := Strip(c.Text); got != c.Want {
			t.Errorf("Strip(%q) = %q, want %q", c.Text, got, c.Want)
		}
	}
}

func TestExtract(t *testing.T) {
	tokens, text := Extract("[item=a]Gleba  [color=red]science [fluid=water,quality=rare] x")
	if !reflect.DeepEqual(tokens, []string{"[item=a]", "[fluid=water]"}) {
		t.Errorf("tokens = %v", tokens)
	}
	if text != "Gleba [color=red]science x" {
		t.Errorf("text = %q", text)
	}
}
