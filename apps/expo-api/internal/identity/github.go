package identity

import (
	"net/http"
	"strconv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

const githubUserURL = "https://api.github.com/user"

// newGitHub signs in with GitHub. No scopes: the public profile (id, login)
// needs none.
func newGitHub(c Credentials, callbackURL string, client *http.Client) Method {
	return &oauthProvider{
		config: oauth2.Config{
			ClientID: c.ClientID, ClientSecret: c.Secret, RedirectURL: callbackURL,
			Endpoint: secretInBody(endpoints.GitHub),
		},
		client:      client,
		userInfoURL: githubUserURL,
		parseUser:   parseGitHubUser,
	}
}

// parseGitHubUser reads GET /user: the numeric id, the login as the hint
// (the display name when there is no login).
func parseGitHubUser(body []byte) (string, string, error) {
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := decodeUser(body, &user); err != nil {
		return "", "", err
	}
	if user.ID <= 0 {
		return "", "", nil
	}
	hint := user.Login
	if hint == "" {
		hint = user.Name
	}
	return strconv.FormatInt(user.ID, 10), hint, nil
}
