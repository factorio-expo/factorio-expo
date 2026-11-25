package portal

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"factorio-expo/apps/expo-game-data/internal/modset"
)

const zipBody = "PK fake zip"

func newServer(t *testing.T, downloads *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/mods/aai-loaders/full":
			w.Write([]byte(`{"releases": [{"version": "0.3.0", "download_url": "/download/aai-loaders/abc",
				"file_name": "aai-loaders_0.3.0.zip", "sha1": "x",
				"info_json": {"factorio_version": "2.1", "dependencies": ["base >= 2.1.6"]}}]}`))
		case r.URL.Path == "/download/aai-loaders/abc":
			if r.URL.Query().Get("token") != "secret-token" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			*downloads++
			w.Write([]byte(zipBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestReleases(t *testing.T) {
	server := newServer(t, new(int))
	defer server.Close()
	client := &Client{HTTP: server.Client(), BaseURL: server.URL}
	releases, err := client.Releases(context.Background(), "aai-loaders")
	if err != nil {
		t.Fatal(err)
	}
	want := modset.Release{
		Version: "0.3.0", FactorioVersion: "2.1", Dependencies: []string{"base >= 2.1.6"},
		DownloadURL: "/download/aai-loaders/abc", FileName: "aai-loaders_0.3.0.zip", SHA1: "x",
	}
	if len(releases) != 1 || releases[0].FileName != want.FileName || releases[0].FactorioVersion != "2.1" ||
		releases[0].Dependencies[0] != want.Dependencies[0] || releases[0].DownloadURL != want.DownloadURL {
		t.Fatalf("releases = %+v", releases)
	}
	if _, err := client.Releases(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "not on the mod portal") {
		t.Fatalf("missing mod error = %v", err)
	}
}

func TestDownloadCachesByChecksum(t *testing.T) {
	downloads := 0
	server := newServer(t, &downloads)
	defer server.Close()
	sum := sha1.Sum([]byte(zipBody))
	release := &modset.Release{
		Version: "0.3.0", DownloadURL: "/download/aai-loaders/abc",
		FileName: "aai-loaders_0.3.0.zip", SHA1: hex.EncodeToString(sum[:]),
	}
	cache := t.TempDir()
	client := &Client{HTTP: server.Client(), BaseURL: server.URL, Username: "me", Token: "secret-token", CacheDir: cache}
	for range 2 {
		path, err := client.Download(context.Background(), "aai-loaders", release)
		if err != nil {
			t.Fatal(err)
		}
		if path != filepath.Join(cache, "aai-loaders_0.3.0.zip") {
			t.Fatalf("path = %s", path)
		}
	}
	if downloads != 1 {
		t.Fatalf("downloaded %d times, want once", downloads)
	}

	// A cached file with another checksum is downloaded again; a bad
	// download is not kept.
	release.SHA1 = "0000"
	if _, err := client.Download(context.Background(), "aai-loaders", release); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, "aai-loaders_0.3.0.zip.tmp")); !os.IsNotExist(err) {
		t.Fatal("a failed download left its temporary file")
	}
}

func TestDownloadErrorsHideTheToken(t *testing.T) {
	sum := sha1.Sum([]byte(zipBody))
	release := &modset.Release{
		Version: "0.3.0", DownloadURL: "/download/aai-loaders/abc",
		FileName: "aai-loaders_0.3.0.zip", SHA1: hex.EncodeToString(sum[:]),
	}
	client := &Client{
		HTTP: http.DefaultClient, BaseURL: "http://127.0.0.1:1", Username: "me", Token: "secret-token",
		CacheDir: t.TempDir(),
	}
	_, err := client.Download(context.Background(), "aai-loaders", release)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error = %v", err)
	}
	client.Token = ""
	if _, err := client.Download(context.Background(), "aai-loaders", release); err == nil || !strings.Contains(err.Error(), "FACTORIO_TOKEN") {
		t.Fatalf("missing credentials error = %v", err)
	}
}
