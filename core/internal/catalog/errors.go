package catalog

import (
	"errors"
	"strconv"
)

type codedError struct {
	code string
	msg  string
	keys []string
}

func (e *codedError) Error() string { return e.code + ": " + e.msg }

func newErr(code, msg string) error { return &codedError{code: code, msg: msg} }

func newStatusErr(code, msg string, status int) error {
	return &codedError{code: code, msg: msg, keys: []string{strconv.Itoa(status)}}
}

func ErrorCode(err error) string {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code
	}
	return ""
}

func ErrorKeys(err error) []string {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.keys
	}
	return nil
}
