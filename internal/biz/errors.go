package biz

import "fmt"

// Error is a client-visible failure. Message is safe to show in the UI.
type Error struct {
	Status   int
	Reason   string
	Message  string
	Fields   map[string]string
	Snapshot any
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Reason
}

func bad(reason, message string, fields map[string]string) *Error {
	return &Error{Status: 400, Reason: reason, Message: message, Fields: fields}
}

func wrap(status int, reason, message string) *Error {
	return &Error{Status: status, Reason: reason, Message: message}
}

func fieldError(fields map[string]string) *Error {
	return &Error{Status: 400, Reason: "VALIDATION", Message: "请检查标出的字段。", Fields: fields}
}

func isBiz(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	be, ok := err.(*Error)
	return be, ok
}

func ozonFailure(err error) *Error {
	if err == nil {
		return nil
	}
	if be, ok := isBiz(err); ok {
		return be
	}
	return &Error{Status: 502, Reason: "OZON_ERROR", Message: err.Error()}
}

func notFound(message string) *Error {
	return &Error{Status: 404, Reason: "NOT_FOUND", Message: message}
}

func fmtID(v int64) string { return fmt.Sprintf("%d", v) }
