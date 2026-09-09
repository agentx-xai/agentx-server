package signature

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestEd25519KeyringSupportsRotationOverlap(t *testing.T) {
	oldPublic, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("immutable artifact")
	verifier, err := NewEd25519Keyring([]string{
		base64.StdEncoding.EncodeToString(newPublic),
		base64.StdEncoding.EncodeToString(oldPublic),
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, privateKey := range map[string]ed25519.PrivateKey{"old": oldPrivate, "new": newPrivate} {
		t.Run(name, func(t *testing.T) {
			signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
			if err := verifier.Verify(context.Background(), payload, signature); err != nil {
				t.Fatalf("rotation key rejected: %v", err)
			}
		})
	}
}

func TestEd25519KeyringRejectsRemovedAndUnknownKeys(t *testing.T) {
	_, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("immutable artifact")
	verifier, err := NewEd25519Keyring([]string{base64.StdEncoding.EncodeToString(newPublic)})
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(oldPrivate, payload))
	if err := verifier.Verify(context.Background(), payload, signature); err == nil {
		t.Fatal("signature from removed key was accepted")
	}
}

func TestEd25519KeyringValidatesEveryConfiguredKey(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEd25519Keyring([]string{base64.StdEncoding.EncodeToString(publicKey), "invalid"}); err == nil {
		t.Fatal("invalid secondary key was accepted")
	}
}
