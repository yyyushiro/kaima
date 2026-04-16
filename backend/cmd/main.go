package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
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

func getAccessTokenHandler(w http.ResponseWriter, r *http.Request) {
	// Get state cookie.
	stateValue, err := GetAndVerifyCookie(r, "state")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			log.Printf("retrieve cookie: %v", err)
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		log.Printf("verify cookie: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
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

	// Verify OpenID Connect
	rawOpenIdToken, ok := tok.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusUnauthorized)
		return
	}

	provider, err := oidc.NewProvider(context.TODO(), "https://accounts.google.com")
	if err != nil {
		log.Printf("setting google as a provider: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
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

	nonceValue, err := GetAndVerifyCookie(r, "nonce")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			log.Printf("retrieve cookie: %v", err)
			http.Error(w, "invalid session", http.StatusUnauthorized)
			return
		}
		log.Printf("verify cookie: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if nonceValue != claims.Nonce {
		log.Printf("the nonce is not valid. Cookie: %s, OpenID: %s", nonceValue, claims.Nonce)
		http.Error(w, "invalid ID token", http.StatusUnauthorized)
		return
	}

	nonceDeleteCookie := makeDeleteCookie("nonce")
	http.SetCookie(w, nonceDeleteCookie)

	sub := claims.Sub

	// Register the user if the user is new. Then, get the user's id.
	var id uuid.UUID
	err = conn.QueryRow(context.Background(), "INSERT INTO users (google_sub) VALUES ($1) ON CONFLICT (google_sub) DO UPDATE SET google_sub = EXCLUDED.google_sub RETURNING id", sub).Scan(&id)
	if err != nil {
		log.Printf("QueryRow failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	signedAccessToken, err := makeSignedAccessToken(id)
	if err != nil {
		log.Printf("making access token: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Make access token Cookie for authorization when the user calls APIs.
	accessTokenCookie := makeSignedCookie("access_token", signedAccessToken, 900)
	http.SetCookie(w, accessTokenCookie)

	// Store refresh token into Redis and Cookie for getting new access token when expired.
	refreshToken, err := generateRefreshToken()
	if err != nil {
		log.Printf("generating refresh token: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

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
