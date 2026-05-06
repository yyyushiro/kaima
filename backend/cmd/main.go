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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
)

func main() {
	// Initialize App struct.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	// defer cancel() makes sure the memory of ctx is released before main() ends.
	defer cancel()

	pool, err := connectDB()
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	rdb, err := newRedisClient()
	if err != nil {
		log.Fatalf("failed to connect to Redis: %v", err)
	}

	oauth2Config, oidcVerifier, err := setUpOAuth2AndOIDC(ctx)
	if err != nil {
		log.Fatalf("failed to configure OAuth2 and OIDC: %v", err)
	}

	app := &App{
		Pool:         pool,
		Rdb:          rdb,
		OAuth2Conf:   oauth2Config,
		OIDCVerifier: oidcVerifier,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/google/start", app.authenticationURIHandler)
	mux.HandleFunc("GET /api/auth/callback/google", app.getAccessTokenHandler)
	mux.HandleFunc("POST /api/posts", app.makePostHandler)
	mux.HandleFunc("GET /api/user/me/posts", app.getMyPostsHandler)
	mux.HandleFunc("DELETE /api/posts/{id}", app.deletePostHandler)
	mux.HandleFunc("POST /api/posts/{id}/likes", app.likePostHandler)
	mux.HandleFunc("DELETE /api/posts/{id}/likes", app.undoLikePostHandler)

	if dir := strings.TrimSpace(os.Getenv("WEB_DIST_DIR")); dir != "" {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			h := spaHandler(dir)
			mux.Handle("GET /{path...}", h)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

// authenticationURIHandler redirects users to google authorization page with making Cookie.
func (a *App) authenticationURIHandler(w http.ResponseWriter, r *http.Request) {
	// Add temporary parameters and construct an authentication request to Google.
	state := rand.Text()
	nonce := rand.Text()

	// Set state and nonce cookies to check validity of the access token and OpenID later.
	http.SetCookie(w, makeSignedCookie("state", state, 300))
	http.SetCookie(w, makeSignedCookie("nonce", nonce, 300))

	oauthURL := a.OAuth2Conf.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("redirect_uri", os.Getenv("REDIRECT_URI")))
	http.Redirect(w, r, oauthURL, http.StatusFound)
}

// getAccessTokenHandler takes the redirect URI, then gets the access token and openID.
func (a *App) getAccessTokenHandler(w http.ResponseWriter, r *http.Request) {
	stateValue, err := getAndVerifyCookie(r, "state")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			log.Printf("retrieve cookie: %v", err)
			http.Error(w, "invalid session", http.StatusUnauthorized)
		}
		log.Printf("verify cookie: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if r.URL.Query().Get("state") != stateValue {
		log.Println("the state is not identical")
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	// Makes sure this state is not reused by attackers.
	stateDeleteCookie := makeDeleteCookie("state")
	http.SetCookie(w, stateDeleteCookie)

	tok, err := a.OAuth2Conf.Exchange(context.TODO(), r.URL.Query().Get("code"))
	if err != nil {
		log.Printf("Exchange Authorization Code: %s", err)
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	rawOpenIdToken, ok := tok.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusUnauthorized)
		return
	}

	openIdToken, err := a.OIDCVerifier.Verify(r.Context(), rawOpenIdToken)
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

	nonceValue, err := getAndVerifyCookie(r, "nonce")
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

	// Make sure the attackers send the app OpneID token with the same nonce and it passes.
	nonceDeleteCookie := makeDeleteCookie("nonce")
	http.SetCookie(w, nonceDeleteCookie)

	sub := claims.Sub

	// Register the user if the user is new. Then, get the user's userId.
	var userId uuid.UUID
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err = a.Pool.QueryRow(ctx, "INSERT INTO users (google_sub) VALUES ($1) ON CONFLICT (google_sub) DO UPDATE SET google_sub = EXCLUDED.google_sub RETURNING id", sub).Scan(&userId)
	if err != nil {
		log.Printf("QueryRow failed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Avoid attacker's changing JWT.
	signedAccessToken, err := makeSignedAccessToken(userId)
	if err != nil {
		log.Printf("making access token: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// This cookie is needed for authorization of user in each request.
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
	ctx, cancel = context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err = a.Rdb.Set(ctx, refreshToken, sub, time.Duration(refreshTokenDuration)*time.Hour).Err()
	if err != nil {
		log.Printf("storing refresh token into Redis: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	refreshTokenCookie := makeSignedCookie("refresh_token", refreshToken, 604800)
	http.SetCookie(w, refreshTokenCookie)

	log.Printf("my access token: %s \n my refresh token: %s", signedAccessToken, refreshToken)

	base := strings.TrimSpace(os.Getenv("APP_PUBLIC_URL"))
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		base = "http://localhost:5173"
	}
	http.Redirect(w, r, base+"/timeline", http.StatusFound)
}

func (a *App) getMyPostsHandler(w http.ResponseWriter, r *http.Request) {
	result, err := RequireAuth(r, a.Rdb)
	if err != nil {
		log.Printf("Authorization: %s", err)
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	posts, err := GetMyPosts(result.Sub, a.Pool, ctx)
	if err != nil {
		log.Printf("Getting current user's posts: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	body, err := json.Marshal(posts)
	if err != nil {
		log.Printf("encode json: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

type makePostRequest struct {
	Body string `json:"body"`
}

func (a *App) makePostHandler(w http.ResponseWriter, r *http.Request) {
	result, err := RequireAuth(r, a.Rdb)
	if err != nil {
		log.Printf("Authorization: %s", err)
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	var req makePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("decode body: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		http.Error(w, "body required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	post, err := AddPost(result.Sub, req.Body, a.Pool, ctx)
	if err != nil {
		log.Printf("creating post: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	body, err := json.Marshal(post)
	if err != nil {
		log.Printf("encode json: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(body); err != nil {
		log.Printf("write response: %v", err)
		return
	}
	log.Printf("made post successfully: %s", req.Body)
}

func (a *App) deletePostHandler(w http.ResponseWriter, r *http.Request) {
	result, err := RequireAuth(r, a.Rdb)
	if err != nil {
		log.Printf("Authorization: %s", err)
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	idStr := r.PathValue("id")
	postID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	post, err := DeletePost(result.Sub, postID, a.Pool, ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("deleting post: %s", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	body, err := json.Marshal(post)
	if err != nil {
		log.Printf("encode json: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

func (a *App) likePostHandler(w http.ResponseWriter, r *http.Request) {
	result, err := RequireAuth(r, a.Rdb)
	if err != nil {
		log.Printf("Authorization: %s", err)
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	postID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	like, err := LikePost(result.Sub, postID, a.Pool, ctx)
	if err != nil {
		log.Printf("Liked a post: %s", err)
		http.Error(w, "faied to like a post", http.StatusInternalServerError)
		return
	}

	body, err := json.Marshal(like)
	if err != nil {
		log.Printf("encode json: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

func (a *App) undoLikePostHandler(w http.ResponseWriter, r *http.Request) {
	result, err := RequireAuth(r, a.Rdb)
	if err != nil {
		log.Printf("Authorization: %s", err)
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	postID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	like, err := UndoLikePost(result.Sub, postID, a.Pool, ctx)
	if err != nil {
		log.Printf("Undid a like: %s", err)
		http.Error(w, "faied to undo a like", http.StatusInternalServerError)
		return
	}

	body, err := json.Marshal(like)
	if err != nil {
		log.Printf("encode json: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		log.Printf("write response: %v", err)
	}
}
