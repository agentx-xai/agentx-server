package apperror

import "errors"

type Kind string

const (
	KindValidation Kind = "validation"
	KindForbidden  Kind = "forbidden"
	KindNotFound   Kind = "not_found"
	KindConflict   Kind = "conflict"
)

type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func New(kind Kind, message string) error {
	return &Error{Kind: kind, Message: message}
}

func Wrap(kind Kind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

func Public(err error) (Kind, string, bool) {
	var public *Error
	if !errors.As(err, &public) {
		return "", "", false
	}
	return public.Kind, public.Message, true
}
