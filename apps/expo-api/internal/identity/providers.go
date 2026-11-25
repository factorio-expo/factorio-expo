package identity

import (
	"fmt"
	"net/http"

	"factorio-expo/apps/expo-api/internal/identity/steam"
)

// Credentials are a provider's client id and secret (Steam: no id, the
// secret is the Web API key, required like every other credential).
type Credentials struct {
	ClientID string
	Secret   string
}

// builder enables one provider. To add a provider, add an entry here: its
// name (the URL segment and the auth_identities.provider value, lowercase
// letters and digits), its label, the environment variables holding its
// credentials, and a constructor returning its Method. Ask only for the
// account's id and public name: no email, no other scopes.
type builder struct {
	name  string
	label string
	// idEnv is empty for a provider without a client id (Steam).
	idEnv     string
	secretEnv string
	build     func(c Credentials, callbackURL string, client *http.Client) Method
}

var builders = []builder{
	{
		name: "github", label: "GitHub",
		idEnv: "API_GITHUB_CLIENT_ID", secretEnv: "API_GITHUB_CLIENT_SECRET",
		build: newGitHub,
	},
	{
		name: "discord", label: "Discord",
		idEnv: "API_DISCORD_CLIENT_ID", secretEnv: "API_DISCORD_CLIENT_SECRET",
		build: newDiscord,
	},
	{
		name: "steam", label: "Steam",
		secretEnv: "API_STEAM_API_KEY",
		build: func(c Credentials, callbackURL string, client *http.Client) Method {
			return steam.New(c.Secret, callbackURL, client)
		},
	},
	{
		name: "google", label: "Google",
		idEnv: "API_GOOGLE_CLIENT_ID", secretEnv: "API_GOOGLE_CLIENT_SECRET",
		build: newGoogle,
	},
}

// credentials reads a builder's environment variables. A provider is enabled
// when all of them are set; setting only some is a configuration error.
func (b builder) credentials(getenv func(string) string) (Credentials, bool, error) {
	c := Credentials{Secret: getenv(b.secretEnv)}
	if b.idEnv != "" {
		c.ClientID = getenv(b.idEnv)
		if (c.ClientID == "") != (c.Secret == "") {
			return Credentials{}, false, fmt.Errorf("set both %s and %s, or neither", b.idEnv, b.secretEnv)
		}
	}
	return c, c.Secret != "", nil
}

func builderByName(name string) (builder, bool) {
	for _, b := range builders {
		if b.name == name {
			return b, true
		}
	}
	return builder{}, false
}
