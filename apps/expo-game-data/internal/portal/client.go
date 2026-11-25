// Package portal talks to the Factorio mod portal: release listings and
// cached, checksum-verified mod downloads.
package portal

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"factorio-expo/apps/expo-game-data/internal/modset"
)

// DefaultBaseURL is the public mod portal.
const DefaultBaseURL = "https://mods.factorio.com"

// Client lists releases and downloads mod zips into CacheDir, where a zip
// with the expected checksum is reused instead of downloaded again.
type Client struct {
	HTTP     *http.Client
	BaseURL  string
	Username string
	Token    string
	CacheDir string
}

type fullResponse struct {
	Releases []struct {
		Version     string `json:"version"`
		DownloadURL string `json:"download_url"`
		FileName    string `json:"file_name"`
		SHA1        string `json:"sha1"`
		InfoJSON    struct {
			FactorioVersion string   `json:"factorio_version"`
			Dependencies    []string `json:"dependencies"`
		} `json:"info_json"`
	} `json:"releases"`
}

// Releases lists every release of a mod (no credentials needed).
func (c *Client) Releases(ctx context.Context, name string) ([]modset.Release, error) {
	endpoint := c.BaseURL + "/api/mods/" + url.PathEscape(name) + "/full"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("list releases of %s: %w", name, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("mod %q is not on the mod portal", name)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list releases of %s: HTTP %d", name, response.StatusCode)
	}
	var full fullResponse
	if err := json.NewDecoder(response.Body).Decode(&full); err != nil {
		return nil, fmt.Errorf("decode releases of %s: %w", name, err)
	}
	releases := make([]modset.Release, 0, len(full.Releases))
	for _, release := range full.Releases {
		releases = append(releases, modset.Release{
			Version:         release.Version,
			FactorioVersion: release.InfoJSON.FactorioVersion,
			Dependencies:    release.InfoJSON.Dependencies,
			DownloadURL:     release.DownloadURL,
			FileName:        release.FileName,
			SHA1:            release.SHA1,
		})
	}
	return releases, nil
}

// Download returns the local path of a release's zip, downloading it only
// when the cache has no copy with the published checksum.
func (c *Client) Download(ctx context.Context, name string, release *modset.Release) (string, error) {
	fileName := filepath.Base(release.FileName)
	if fileName == "." || fileName == "/" || !strings.HasSuffix(fileName, ".zip") {
		return "", fmt.Errorf("mod %s %s: unexpected file name %q", name, release.Version, release.FileName)
	}
	path := filepath.Join(c.CacheDir, fileName)
	if sum, err := fileSHA1(path); err == nil && sum == release.SHA1 {
		return path, nil
	}
	if c.Username == "" || c.Token == "" {
		return "", fmt.Errorf("downloading mod %s needs FACTORIO_USERNAME and FACTORIO_TOKEN", name)
	}
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		return "", err
	}
	log.Printf("downloading mod %s %s...", name, release.Version)
	query := url.Values{"username": {c.Username}, "token": {c.Token}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+release.DownloadURL+"?"+query.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("download mod %s: bad download URL", name)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return "", fmt.Errorf("download mod %s: %w", name, withoutURL(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download mod %s: HTTP %d (check FACTORIO_USERNAME and FACTORIO_TOKEN)", name, response.StatusCode)
	}
	temporary := path + ".tmp"
	file, err := os.Create(temporary)
	if err != nil {
		return "", err
	}
	hash := sha1.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), response.Body)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		os.Remove(temporary)
		return "", fmt.Errorf("download mod %s: %w", name, withoutURL(err))
	}
	if sum := hex.EncodeToString(hash.Sum(nil)); sum != release.SHA1 {
		os.Remove(temporary)
		return "", fmt.Errorf("download mod %s %s: checksum %s, portal says %s", name, release.Version, sum, release.SHA1)
	}
	if err := os.Rename(temporary, path); err != nil {
		return "", err
	}
	return path, nil
}

// withoutURL drops the request URL from an error: it carries the token.
func withoutURL(err error) error {
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return urlErr.Err
	}
	return err
}

func fileSHA1(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
