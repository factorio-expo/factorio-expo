package identity

import (
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

const discordUserURL = "https://discord.com/api/users/@me"

// newDiscord signs in with Discord, scope identify (id and names, no email).
func newDiscord(c Credentials, callbackURL string, client *http.Client) Method {
	return &oauthProvider{
		config: oauth2.Config{
			ClientID: c.ClientID, ClientSecret: c.Secret, RedirectURL: callbackURL,
			Endpoint: secretInBody(endpoints.Discord), Scopes: []string{"identify"},
		},
		client:      client,
		userInfoURL: discordUserURL,
		parseUser:   parseDiscordUser,
	}
}

// parseDiscordUser reads GET /users/@me: the snowflake id, the username as
// the hint (the display name when there is no username).
func parseDiscordUser(body []byte) (string, string, error) {
	var user struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
	}
	if err := decodeUser(body, &user); err != nil {
		return "", "", err
	}
	hint := user.Username
	if hint == "" {
		hint = user.GlobalName
	}
	return user.ID, hint, nil
}
