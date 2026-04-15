package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var oauthConf, oidcConf = MakeGoogleOAuth2Config()

var secretKey = os.Getenv("HMAC_SECRET_KEY")

type UserInfo struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/google/start", authenticationURIHandler)
	mux.HandleFunc("/api/auth/callback/google", getAccessTokenHandler)

	http.ListenAndServe(":8080", mux)
}

// SignCookie sets a signature on a given Cookie using HMAC.
func SignCookie(c *http.Cookie) *http.Cookie {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(c.Name))
	mac.Write([]byte(c.Value))
	signature := mac.Sum(nil)

	c.Value = hex.EncodeToString(signature) + c.Value
	return c
}

func makeSignedCookie(name, value string) *http.Cookie {
	c := http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   false, // implement HTTPS and turn it true later.
		SameSite: http.SameSiteLaxMode,
	}
	return SignCookie(&c)
}

// authenticationURIHandler redirects users to google authorization page with making Cookie.
func authenticationURIHandler(w http.ResponseWriter, r *http.Request) {
	// Add temporary parameters and construct an authentication request to Google.
	state := rand.Text()
	nonce := rand.Text()

	http.SetCookie(w, makeSignedCookie("state", state))
	http.SetCookie(w, makeSignedCookie("nonce", nonce))

	oauthURL := oauthConf.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("redirect_uri", os.Getenv("REDIRECT_URI")))
	http.Redirect(w, r, oauthURL, http.StatusFound)
	log.Println("Redirect to authorization page.")
}

// ValidateCookie decodes a given signed Cookie with HMAC and validate it.
func ValidateCookie(signedCookie *http.Cookie) (string, error) {
	hexSize := sha256.Size * 2
	signature, err := hex.DecodeString(signedCookie.Value[:hexSize])
	if err != nil {
		return "", errors.New("Failed to Decode")
	}
	value := signedCookie.Value[hexSize:]

	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(signedCookie.Name))
	mac.Write([]byte(value))
	expectedSignature := mac.Sum(nil)
	if !hmac.Equal(signature, expectedSignature) {
		return "", errors.New("invalid signature")
	}
	return value, nil
}

func getAccessTokenHandler(w http.ResponseWriter, r *http.Request) {
	// Get state cookie.
	signedStateCookie, err := r.Cookie("state")
	if err != nil {
		if err == http.ErrNoCookie {
			log.Printf("cookie state: %v", err)
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		log.Printf("cookie state: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	stateValue, err := ValidateCookie(signedStateCookie)
	if err != nil {
		log.Printf("State Validation: %s", err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Check if the state from Cookie is the same as the one from URL.
	q := r.URL.Query()
	if q.Get("state") != stateValue {
		log.Println("the state is not identical")
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	// Exchange the authorization code with an access token.
	tok, err := oauthConf.Exchange(context.TODO(), q.Get("code"))
	if err != nil {
		log.Printf("Exchange Authorization Code: %s", err)
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	// Get private information within the scopes.
	client := oauthConf.Client(context.TODO(), tok)

	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		log.Printf("getting userinfo: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		log.Printf("getting userinfo: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	email := userInfo.Email

	rawOpenIdToken, ok := tok.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusUnauthorized)
		return
	}

	// Verify openID Connect.
	provider, _ := oidc.NewProvider(context.TODO(), "https://accounts.google.com")
	verifier := provider.Verifier(oidcConf)

	openIdToken, err := verifier.Verify(context.TODO(), rawOpenIdToken)
	if err != nil {
		http.Error(w, "invalid OpenID Token", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Nonce string `json:"nonce"`
		Sub   string `json:"sub"`
	}

	if err := openIdToken.Claims(&claims); err != nil {
		log.Printf("gettin nonce: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Get the nonce Cookie.
	signedNonceCookie, err := r.Cookie("nonce")
	if err != nil {
		if err == http.ErrNoCookie {
			log.Printf("cookie nonce: %v", err)
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		log.Printf("cookie nonce: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Check if the cookie is valid.
	nonceValue, err := ValidateCookie(signedNonceCookie)
	if err != nil {

	}

	// Check if the nonce from Cookie is identical to the one from ID token.
	if nonceValue != claims.Nonce {
		log.Println("the nonce is not valid.")
		http.Error(w, "invalid ID token", http.StatusUnauthorized)
		return
	}

	// Delete the used nonce.
	nonceDeleteCookie := &http.Cookie{
		Name:     "nonce",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   false, // implement HTTPS and turn it true later.
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, nonceDeleteCookie)

	// Print access token and email for testing
	log.Printf("Email: %s, Sub; %s", email, claims.Sub)
}

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
