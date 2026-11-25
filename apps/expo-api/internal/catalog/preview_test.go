package catalog

import (
	"testing"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// A preview that fits the thumbnail size is stored as its thumbnail alone, so
// its full URL is the thumbnail's: browsers and caches keep one copy.
func TestPreviewOfPointsSmallPreviewsAtTheThumbnail(t *testing.T) {
	small := previewOf("b", db.ListPreviewsByRevisionRow{Width: 800, Height: 600})
	if small.URL != ThumbnailURL("b") || small.ThumbnailWidth != 800 || small.ThumbnailHeight != 600 ||
		!small.FullAvailable {
		t.Fatalf("small = %+v", small)
	}
	large := previewOf("b", db.ListPreviewsByRevisionRow{Width: 1600, Height: 600})
	if large.URL != PreviewURL("b") || large.ThumbnailURL != ThumbnailURL("b") ||
		large.ThumbnailWidth != 800 || large.ThumbnailHeight != 300 || large.FullAvailable {
		t.Fatalf("large = %+v", large)
	}
	stored := previewOf("b", db.ListPreviewsByRevisionRow{Width: 1600, Height: 600, FullStored: true})
	if !stored.FullAvailable {
		t.Fatalf("stored = %+v", stored)
	}
}
