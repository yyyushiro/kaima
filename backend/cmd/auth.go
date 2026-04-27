package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// generateRefreshToken generates a random 32-byte base64 token for a refresh token.
func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// makeSignedAccessToken generates generates JWT with the given sub.
func makeSignedAccessToken(sub uuid.UUID) (string, error) {
	accessTokenDuration, err := strconv.Atoi(os.Getenv("ACCESS_TOKEN_DURATION"))
	if err != nil {
		return "", err
	}

	exp := time.Now().Add(time.Duration(accessTokenDuration) * time.Minute)
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256,
		jwt.MapClaims{
			"sub": sub,
			"exp": exp.Unix(),
			"iat": time.Now().Unix(),
		})

	signedAccessToken, err := accessToken.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		return "", err
	}
	return signedAccessToken, nil
}

type accessTokenClaims struct {
	Sub string `json:"sub"`
	jwt.RegisteredClaims
}

// verifyAccessTokenJWT verifies the given JWT and decode it.
func verifyAccessTokenJWT(tokenString string, secret []byte) (*accessTokenClaims, error) {
	var claims accessTokenClaims
	tok, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected alg: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return &claims, nil
}

// You can expand this struct if you want to get more information from authorization later.
type AuthResult struct {
	Sub                  uuid.UUID
	newAccessTokenCookie *http.Cookie
}

// RequireAuth is used in every API call and verifies the user's access token.
func RequireAuth(r *http.Request, rdb *redis.Client) (*AuthResult, error) {
	accessToken, err := getAndVerifyCookie(r, "access_token")
	if err != nil {
		return nil, fmt.Errorf("Verifying access token Cookie: %w", err)
	}

	accessTokenClaims, err := verifyAccessTokenJWT(accessToken, []byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		return nil, fmt.Errorf("Verifying access token JWT: %w", err)
	}

	subUuid, err := uuid.Parse(accessTokenClaims.Sub)
	if err != nil {
		return nil, fmt.Errorf("parsing string into uuid: %w", err)
	}

	// verify exp this is happy route
	if accessTokenClaims.ExpiresAt.Time.Unix() > time.Now().Unix() {
		return &AuthResult{Sub: subUuid, newAccessTokenCookie: nil}, nil
	}

	// if exp is invalid, then check refreshtoken.
	refreshToken, err := getAndVerifyCookie(r, "refresh_token")
	if err != nil {
		return nil, fmt.Errorf("Verifying refresh token Cookie: %w", err)
	}

	// get sub by redis and refreshtoken. If it is not found, then expired.
	subStr, err := rdb.Get(context.Background(), refreshToken).Result()
	if err != nil {
		return nil, fmt.Errorf("Getting sub from redis: %w", err)
	}

	subUuid, err = uuid.Parse(subStr)
	if err != nil {
		return nil, fmt.Errorf("Parsing sub into uuid: %w", err)
	}

	// found sub, so issue new access token.
	newSignedAccessToken, err := makeSignedAccessToken(subUuid)
	if err != nil {
		return nil, fmt.Errorf("Making new access token: %w", err)
	}

	// return the new access token cookie.
	return &AuthResult{
		Sub:                  subUuid,
		newAccessTokenCookie: makeSignedCookie("access_token", newSignedAccessToken, 900),
	}, nil
}
