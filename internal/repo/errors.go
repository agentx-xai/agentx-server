package repo

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound            = errors.New("resource not found")
	ErrConflict            = errors.New("resource conflict")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
)

func NotFound(resource string) error {
	return fmt.Errorf("%s not found: %w", resource, ErrNotFound)
}

func Conflict(message string) error {
	return fmt.Errorf("%s: %w", message, ErrConflict)
}

func IdempotencyConflict(message string) error {
	return fmt.Errorf("%s: %w", message, ErrIdempotencyConflict)
}
