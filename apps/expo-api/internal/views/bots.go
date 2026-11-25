package views

import "github.com/x-way/crawlerdetect"

// IsBot reports a user agent that is not a person's browser: known crawlers,
// link unfurlers, HTTP libraries and headless browsers, or none at all.
func IsBot(userAgent string) bool {
	return userAgent == "" || crawlerdetect.IsCrawler(userAgent)
}
