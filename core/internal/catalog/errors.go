package catalog

import "errors"

type codedError struct {
	code string
	msg  string
}

func (e *codedError) Error() string { return e.code + ": " + e.msg }

func newErr(code, msg string) error { return &codedError{code: code, msg: msg} }

func ErrorCode(err error) string {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	return ""
}
