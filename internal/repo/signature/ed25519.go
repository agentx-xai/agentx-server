package signature

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

type Ed25519 struct{ PublicKey ed25519.PublicKey }

func NewEd25519(encoded string) (*Ed25519, error) {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, errors.New("ed25519 public key must be 32 bytes")
	}
	return &Ed25519{PublicKey: b}, nil
}
func (v *Ed25519) Verify(_ context.Context, payload []byte, encoded string) error {
	sig, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	if !ed25519.Verify(v.PublicKey, payload, sig) {
		return errors.New("invalid artifact signature")
	}
	return nil
}
