package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

type tokenService struct {
	secret []byte
	ttl    time.Duration
}

type tokenClaims struct {
	Subject   string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

func newTokenService(secret string, ttl time.Duration) tokenService {
	if secret == "" {
		secret = "live-auction-development-secret"
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return tokenService{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

func (s tokenService) Sign(userID string, now time.Time) (string, error) {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	claims := tokenClaims{
		Subject:   userID,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(s.ttl).Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	return unsigned + "." + s.signature(unsigned), nil
}

func (s tokenService) Verify(token string, now time.Time) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errorx.ErrUnauthorized
	}

	unsigned := parts[0] + "." + parts[1]
	want := s.signature(unsigned)
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return "", errorx.ErrUnauthorized
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errorx.ErrUnauthorized
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errorx.ErrUnauthorized
	}
	if claims.Subject == "" || claims.ExpiresAt <= now.Unix() {
		return "", errorx.ErrUnauthorized
	}
	return claims.Subject, nil
}

func (s tokenService) signature(unsigned string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
