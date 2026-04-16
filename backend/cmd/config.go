package main

import (
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// MakeGoogleOAuth2Config gets environment variables
// and make the configuration of Google OAuth2.0 with the scope and endpoint.
func MakeGoogleOAuth2Config() (*oauth2.Config, *oidc.Config) {

	clientId := os.Getenv("CLIENT_ID")
	clientSecret := os.Getenv("CLIENT_SECRET")
	redirectUri := os.Getenv("REDIRECT_URI")

	conf := &oauth2.Config{
		ClientID:     clientId,
		ClientSecret: clientSecret,
		RedirectURL:  redirectUri,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
			"openid",
		},
		Endpoint: google.Endpoint,
	}
	return conf, &oidc.Config{ClientID: clientId}
}
