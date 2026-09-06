package auth

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Principal struct{ UserID, Issuer, Subject, Email string }
type contextKey struct{}
type requestIDKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func VerifyBearer(raw, apiToken, jwtSecret, issuer, audience string) (Principal, error) {
	if apiToken != "" && hmac.Equal([]byte(raw), []byte(apiToken)) {
		return Principal{UserID: "token", Subject: "token"}, nil
	}
	if jwtSecret == "" {
		return Principal{}, errors.New("unauthorized")
	}
	options := make([]jwt.ParserOption, 0, 2)
	if issuer != "" {
		options = append(options, jwt.WithIssuer(issuer))
	}
	if audience != "" {
		options = append(options, jwt.WithAudience(audience))
	}
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(jwtSecret), nil
	}, options...)
	if err != nil || !tok.Valid {
		return Principal{}, errors.New("invalid bearer token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return Principal{}, errors.New("invalid claims")
	}
	sub, _ := claims.GetSubject()
	if sub == "" {
		return Principal{}, errors.New("subject is required")
	}
	iss, _ := claims["iss"].(string)
	email, _ := claims["email"].(string)
	return Principal{UserID: iss + "|" + sub, Issuer: iss, Subject: sub, Email: email}, nil
}

func ParseUnsignedClaims(raw string) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid jwt")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c map[string]any
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if exp, ok := c["exp"].(float64); ok && time.Now().Unix() >= int64(exp) {
		return nil, errors.New("token expired")
	}
	return c, nil
}
