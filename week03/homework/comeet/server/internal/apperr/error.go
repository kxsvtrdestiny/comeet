package apperr

import "errors"

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string                   { return e.Code }
func New(status int, code, message string) error { return &Error{status, code, message} }
func Invalid(message string) error               { return New(400, "VALIDATION_ERROR", message) }
func IsUnique(err error) bool {
	var c interface{ Code() int }
	return errors.As(err, &c) && c.Code() == 2067
}
