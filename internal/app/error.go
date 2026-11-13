package app

import "fmt"

type Error struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func fail(status int, code, message string, details any) *Error {
	return &Error{Status: status, Code: code, Message: message, Details: details}
}

func asError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	return fail(500, "INTERNAL", err.Error(), nil)
}

func fmtErr(code, format string, args ...any) *Error {
	return fail(400, code, fmt.Sprintf(format, args...), nil)
}
