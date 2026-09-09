package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"testing"
	"time"
)

func TestVerifyBearerHMACClaims(t *testing.T) {
	secret := "test-secret"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user-1", "iss": "issuer", "aud": "audience", "exp": time.Now().Add(time.Minute).Unix(), "email": "u@example.com", "email_verified": true})
	raw, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	p, err := VerifyBearer(raw, "", secret, "issuer", "audience")
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != "issuer|user-1" || p.Email != "u@example.com" || !p.EmailVerified {
		t.Fatalf("unexpected principal: %+v", p)
	}
}

func TestVerifyBearerDoesNotInferEmailVerification(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user-1", "iss": "issuer", "aud": "audience", "exp": time.Now().Add(time.Minute).Unix(), "email": "u@example.com", "email_verified": "true"})
	raw, _ := token.SignedString([]byte("test-secret"))
	principal, err := VerifyBearer(raw, "", "test-secret", "issuer", "audience")
	if err != nil {
		t.Fatal(err)
	}
	if principal.EmailVerified {
		t.Fatal("non-boolean email_verified claim was trusted")
	}
}

func TestVerifyBearerRejectsWrongAudience(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "user-1", "iss": "issuer", "aud": "other", "exp": time.Now().Add(time.Minute).Unix()})
	raw, _ := token.SignedString([]byte("test-secret"))
	if _, err := VerifyBearer(raw, "", "test-secret", "issuer", "audience"); err == nil {
		t.Fatal("expected audience validation failure")
	}
}
