package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
)

// App struct holds all dependent variables.
type App struct {
	Pool         *pgxpool.Pool
	Rdb          *redis.Client
	OAuth2Conf   *oauth2.Config
	OIDCVerifier *oidc.IDTokenVerifier
}

func connectDB() (*pgxpool.Pool, error) {
	return pgxpool.New(context.Background(), os.Getenv("POSTGRES_URL"))
}

func newRedisClient() (*redis.Client, error) {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		host := strings.TrimSpace(os.Getenv("REDIS_HOST"))
		port := strings.TrimSpace(os.Getenv("REDIS_PORT"))
		if host != "" && port != "" {
			addr = host + ":" + port
		}
	}
	if addr == "" {
		addr = "redis:6379"
	}

	opts := &redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Ping() connects to Redis in practice and makes sure the connection is sound.
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return client, nil
}

// setUpOAuth2AndOIDC gets environment variables
// and make the configuration of Google OAuth2.0 with the scope and endpoint.
func setUpOAuth2AndOIDC(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {

	clientId := os.Getenv("CLIENT_ID")

	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, nil, err
	}

	oauth2Conf := &oauth2.Config{
		ClientID:     clientId,
		ClientSecret: os.Getenv("CLIENT_SECRET"),
		RedirectURL:  os.Getenv("REDIRECT_URI"),
		Scopes: []string{
			"openid",
		},
		Endpoint: provider.Endpoint(),
	}

	// oidcConf needs the clientId so that the returned access_token is actually for this app.
	// If it does not check clientId in JWT, then it is possible that
	// the attacker sends unrelated access token to your app and your app makes the user log in.
	oidcConf := oidc.Config{ClientID: clientId}

	oidcVerifier := provider.Verifier(&oidcConf)

	return oauth2Conf, oidcVerifier, nil
}
