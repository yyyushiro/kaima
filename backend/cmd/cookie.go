package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
)

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

// GetAndVerifyCookie verifies the given name's Cookie and return its decoded value.
func GetAndVerifyCookie(r *http.Request, name string) (string, error) {
	signedCookie, err := r.Cookie(name)
	if err != nil {
		return "", err
	}
	hexSize := sha256.Size * 2

	signature, err := hex.DecodeString(signedCookie.Value[:hexSize])
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
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
