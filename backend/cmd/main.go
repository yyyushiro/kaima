package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var oauthConf, oidcConf = MakeGoogleOAuth2Config()

var secretKey = os.Getenv("HMAC_SECRET_KEY")

var conn *pgx.Conn

var rdb = redis.NewClient(&redis.Options{
	Addr:     "redis:6379",
	Password: "",
	DB:       0,
	Protocol: 2,
})

func initDB() {
	var err error
	conn, err = pgx.Connect(context.Background(), os.Getenv("POSTGRES_URL"))
	if err != nil {
		log.Printf("Unable to connect to database: %v", err)
		os.Exit(1)
	}
}

type UserInfo struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func main() {
	initDB()
	defer conn.Close(context.Background())
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

func makeSignedCookie(name, value string, maxAge int) *http.Cookie {
	c := http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   false, // implement HTTPS and turn it true later.
		SameSite: http.SameSiteLaxMode,
	}
	return SignCookie(&c)
}

func makeDeleteCookie(name string) *http.Cookie {
	c := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   false, // implement HTTPS and turn it true later.
		SameSite: http.SameSiteLaxMode,
	}
	return c
}

// authenticationURIHandler redirects users to google authorization page with making Cookie.
func authenticationURIHandler(w http.ResponseWriter, r *http.Request) {
	// Add temporary parameters and construct an authentication request to Google.
	state := rand.Text()
	nonce := rand.Text()

	http.SetCookie(w, makeSignedCookie("state", state, 300))
	http.SetCookie(w, makeSignedCookie("nonce", nonce, 300))

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

	stateDeleteCookie := makeDeleteCookie("state")
	http.SetCookie(w, stateDeleteCookie)

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
	nonceDeleteCookie := makeDeleteCookie("nonce")
	http.SetCookie(w, nonceDeleteCookie)

	sub := claims.Sub

	// Print access token and email for testing
	log.Printf("Email: %s, Sub; %s", email, sub)

	// register the user if the user is new. Then, get the user's id.
	var id uuid.UUID
	err = conn.QueryRow(context.Background(), "INSERT INTO users (google_sub) VALUES ($1) ON CONFLICT (google_sub) DO UPDATE SET google_sub = EXCLUDED.google_sub RETURNING id", sub).Scan(&id)
	if err != nil {
		log.Printf("QueryRow failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// generate my app's access token (JWT style) using the sub.
	accessTokenDuration, err := strconv.Atoi(os.Getenv("ACCESS_TOKEN_DURATION"))
	if err != nil {
		log.Printf("parsing time duration: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	exp := time.Now().Add(time.Duration(accessTokenDuration) * time.Minute)
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256,
		jwt.MapClaims{
			"sub": id,
			"exp": exp.Unix(),
			"iat": time.Now().Unix(),
		})

	signedAccessToken, err := accessToken.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		log.Printf("signing jwt: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// create cookie for access_token and refresh token.
	accessTokenCookie := makeSignedCookie("access_token", signedAccessToken, 900)
	http.SetCookie(w, accessTokenCookie)

	// generate refresh token.
	refreshToken, err := generateRefreshToken()
	if err != nil {
		log.Printf("generating refresh token: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// store refresh token into Redis and Cookie.
	refreshTokenDuration, err := strconv.Atoi(os.Getenv("REFRESH_TOKEN_DURATION"))
	if err != nil {
		log.Printf("parsing refresh token duration: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	err = rdb.Set(context.Background(), refreshToken, sub, time.Duration(refreshTokenDuration)*time.Hour).Err()
	if err != nil {
		log.Printf("storing refresh token into Redis: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	refreshTokenCookie := makeSignedCookie("refresh_token", refreshToken, 604800)
	http.SetCookie(w, refreshTokenCookie)

	log.Printf("my access token: %s \n my refresh token: %s", signedAccessToken, refreshToken)

	http.Redirect(w, r, "http://localhost:5173/timeline", http.StatusFound)
}

func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
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
