package identity

import (
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

const googleUserURL = "https://www.googleapis.com/oauth2/v2/userinfo"

// newGoogle signs in with Google, scope profile (id and name, no email).
// It uses the shared endpoints table, not golang.org/x/oauth2/google, which
// pulls in Google Cloud credential code the sign-in does not need.
func newGoogle(c Credentials, callbackURL string, client *http.Client) Method {
	return &oauthProvider{
		config: oauth2.Config{
			ClientID: c.ClientID, ClientSecret: c.Secret, RedirectURL: callbackURL,
			Endpoint: secretInBody(endpoints.Google), Scopes: []string{"profile"},
		},
		client:      client,
		userInfoURL: googleUserURL,
		parseUser:   parseGoogleUser,
	}
}

// parseGoogleUser reads the userinfo answer: the account id and the name as
// the hint.
func parseGoogleUser(body []byte) (string, string, error) {
	var user struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := decodeUser(body, &user); err != nil {
		return "", "", err
	}
	return user.ID, user.Name, nil
}
