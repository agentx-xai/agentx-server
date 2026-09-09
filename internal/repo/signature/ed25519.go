package signature

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
)

type Ed25519 struct{ publicKeys []ed25519.PublicKey }

func NewEd25519(encoded string) (*Ed25519, error) {
	return NewEd25519Keyring([]string{encoded})
}

func NewEd25519Keyring(encoded []string) (*Ed25519, error) {
	if len(encoded) == 0 {
		return nil, errors.New("at least one ed25519 public key is required")
	}
	keys := make([]ed25519.PublicKey, 0, len(encoded))
	seen := make(map[string]struct{}, len(encoded))
	for index, value := range encoded {
		b, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("public key %d: %w", index+1, err)
		}
		if len(b) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("public key %d: ed25519 public key must be 32 bytes", index+1)
		}
		fingerprint := string(b)
		if _, exists := seen[fingerprint]; exists {
			continue
		}
		seen[fingerprint] = struct{}{}
		keys = append(keys, ed25519.PublicKey(b))
	}
	return &Ed25519{publicKeys: keys}, nil
}
func (v *Ed25519) Verify(_ context.Context, payload []byte, encoded string) error {
	sig, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	for _, publicKey := range v.publicKeys {
		if ed25519.Verify(publicKey, payload, sig) {
			return nil
		}
	}
	return errors.New("invalid artifact signature")
}
