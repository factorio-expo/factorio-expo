package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

func (s *server) routes() http.Handler {
	serveMux := http.NewServeMux()
	s.api = newAPI(serveMux)
	registerHealth(s.api, "api")
	s.registerOperations(huma.NewGroup(s.api, publicPrefix))
	return withRequestLogging(serveMux)
}

// registerOperations registers the operations moved to Huma (openapi.go).
func (s *server) registerOperations(api huma.API) {
	s.registerFavorites(api)
	s.registerRevisions(api)
	s.registerEntryWrites(api)
	s.registerImports(api)
	s.registerAliases(api)
	s.registerFeed(api)
	s.registerPreviews(api)
	s.registerNotes(api)
	s.registerComments(api)
	s.registerReports(api)
	s.registerViews(api)
	s.registerTools(api)
	s.registerPreviewRender(api)
	s.registerProfile(api)
	s.registerCollections(api)
	s.registerChallenges(api)
	s.registerTrending(api)
	s.registerLeaderboards(api)
	s.registerSite(api)
	s.registerRunnerHealth(api)
	s.registerAuth(api)
	s.registerProviders(api)
	s.registerSignup(api)
	s.registerPasskeys(api)
	s.registerRecover(api)
	s.registerSignInMethods(api)
	s.registerReauth(api)
	s.registerAccountDelete(api)
	s.registerSessions(api)
	s.registerAccountExport(api)
	s.registerSitemap(api)
	s.registerUsers(api)
	s.registerSearch(api)
	s.registerCompare(api)
	s.registerEntryReads(api)
	s.registerEntryMetadata(api)
	s.registerEntryEvents(api)
	s.registerTags(api)
	s.registerModsets(api)
}
