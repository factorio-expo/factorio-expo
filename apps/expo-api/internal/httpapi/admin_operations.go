package httpapi

import "github.com/danielgtaylor/huma/v2"

// registerOperations registers the staff listener's operations (each group's
// register function, in its own file).
func (s *adminServer) registerOperations(api huma.API) {
	s.registerAdminEntries(api)
	s.registerBans(api)
	s.registerModeration(api)
	s.registerProblemReports(api)
	s.registerAdminTags(api)
	s.registerAdminJobs(api)
	s.registerAdminCleanups(api)
	s.registerAdminOverview(api)
	s.registerAdminAudit(api)
	s.registerAdminChallenges(api)
	s.registerAdminUsers(api)
	s.registerAdminSettings(api)
	s.registerAdminHomePicks(api)
}
