package main

import (
	"log/slog"

	"factorio-expo/apps/expo-api/internal/config"
	"factorio-expo/apps/expo-api/internal/importer"
)

// maxImportBytes is the largest exchange string an import accepts: the upload
// body limit.
const maxImportBytes = 8 << 20

// newImporter builds the import fetcher over the supported sites. With
// API_IMPORT_STUB_ORIGIN (local and e2e stacks only, config.checkImportStub)
// every site is a path of that stand-in server instead.
func newImporter(cfg config.Config) *importer.Fetcher {
	sites := importer.DefaultSites()
	private := false
	if cfg.ImportStubOrigin != "" {
		slog.Warn("imports are served by a stand-in server (API_IMPORT_STUB_ORIGIN)", "origin", cfg.ImportStubOrigin)
		sites, private = importer.RebaseSites(sites, cfg.ImportStubOrigin), true
	}
	fetcher, err := importer.New(importer.Config{
		Sites: sites, MaxExchangeBytes: maxImportBytes, AllowPrivateNetworks: private,
		HourlyBudget: cfg.ImportHourlyBudget, SiteBudgets: map[string]int{"gist": cfg.ImportGistHourlyBudget},
	})
	if err != nil {
		// The site table is code, checked by tests.
		panic(err)
	}
	return fetcher
}
