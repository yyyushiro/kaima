package main

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func generateRefreshToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func makeSignedAccessToken(sub uuid.UUID) (string, error) {
	// Generate my app's access token (JWT style) using the sub.
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
