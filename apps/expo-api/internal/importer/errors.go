package importer

import "factorio-expo/apps/expo-api/internal/fault"

// Every refusal is a coded fault the API answers with its own status and a
// message written for the person who pasted the link. Network faults of the
// other side answer 503, everything wrong with the link or what it holds 422.
var (
	// ErrInvalidLink is a link that is not a plain https URL.
	ErrInvalidLink = fault.New(fault.Unprocessable, "import_invalid_link", "that is not a valid https link")
	// ErrUnsupportedSite is a link to a host no adapter handles, or a path
	// its adapter does not know.
	ErrUnsupportedSite = fault.New(fault.Unprocessable, "import_unsupported_site",
		"links from that site are not supported")
	// ErrSourceNotFound is a link its site does not know (or may not show).
	ErrSourceNotFound = fault.New(fault.Unprocessable, "import_source_not_found",
		"the site has nothing at that link (is it public?)")
	// ErrNoBlueprint is a page that holds no exchange string.
	ErrNoBlueprint = fault.New(fault.Unprocessable, "import_no_blueprint",
		"no blueprint string was found at that link")
	// ErrResponseTooLarge is an answer past the size limit.
	ErrResponseTooLarge = fault.New(fault.Unprocessable, "import_too_large", "the linked page is too large")
	// ErrFetchFailed is a site that could not be reached in time or answered
	// with an error of its own.
	ErrFetchFailed = fault.New(fault.Unavailable, "import_fetch_failed",
		"the site could not be reached, try again later")
	// ErrSourceRateLimited is a site that refused for asking too often.
	ErrSourceRateLimited = fault.New(fault.Unavailable, "import_source_rate_limited",
		"the site is limiting requests right now, try again later")
	// ErrBusy is too many imports running at once.
	ErrBusy = fault.New(fault.Unavailable, "import_busy", "too many imports are running, try again in a moment")
	// ErrSiteBusy is the fetch budget of one site spent for the hour.
	ErrSiteBusy = fault.New(fault.Unavailable, "import_site_busy",
		"too many imports from that site right now, try again in a while")
)
