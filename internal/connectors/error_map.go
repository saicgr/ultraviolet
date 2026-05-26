package connectors

import (
	"errors"
	"strings"

	"google.golang.org/api/googleapi"
)

// SQLState bundles a PG SQLSTATE code + human-readable message. Returned from
// MapWarehouseError so the pgwire layer can emit a protocol-correct ErrorResponse
// instead of the catch-all XX000 fallback flagged in AUDIT.md §4.
//
// SQLState implements `error` and exposes `SQLState() string` so pgwire's
// errSQLState walker can pluck the code without importing this package.
type SQLState struct {
	Code    string
	Message string
	Wrapped error
}

func (s SQLState) Error() string {
	if s.Message != "" {
		return s.Message
	}
	if s.Wrapped != nil {
		return s.Wrapped.Error()
	}
	return s.Code
}

func (s SQLState) SQLState() string { return s.Code }
func (s SQLState) Unwrap() error    { return s.Wrapped }

// WrapWarehouseError annotates err with a PG SQLSTATE code derived from
// MapWarehouseError; nil err returns nil. Connectors should call this on every
// error path so pgwire emits the right code.
func WrapWarehouseError(err error) error {
	if err == nil {
		return nil
	}
	s := MapWarehouseError(err)
	s.Wrapped = err
	return s
}

// MapWarehouseError converts a connector error into the closest Postgres SQLSTATE.
// Per docs/conventions/error-mapping.md.
func MapWarehouseError(err error) SQLState {
	if err == nil {
		return SQLState{Code: "00000", Message: ""}
	}

	// BigQuery (googleapi.Error) HTTP-status-driven mapping.
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		return mapHTTPStatus(ge.Code, ge.Message)
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden"):
		return SQLState{Code: "42501", Message: err.Error()} // insufficient_privilege
	case strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist"):
		return SQLState{Code: "42P01", Message: err.Error()} // undefined_table
	case strings.Contains(msg, "syntax") || strings.Contains(msg, "parse error"):
		return SQLState{Code: "42601", Message: err.Error()} // syntax_error
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return SQLState{Code: "57014", Message: err.Error()} // query_canceled
	case strings.Contains(msg, "quota") || strings.Contains(msg, "rate limit"):
		return SQLState{Code: "53400", Message: err.Error()} // configuration_limit_exceeded
	case strings.Contains(msg, "connection") || strings.Contains(msg, "network"):
		return SQLState{Code: "08006", Message: err.Error()} // connection_failure
	}
	return SQLState{Code: "XX000", Message: err.Error()} // internal_error
}

func mapHTTPStatus(code int, msg string) SQLState {
	switch {
	case code == 400:
		return SQLState{Code: "42601", Message: msg}
	case code == 401, code == 403:
		return SQLState{Code: "42501", Message: msg}
	case code == 404:
		return SQLState{Code: "42P01", Message: msg}
	case code == 408:
		return SQLState{Code: "57014", Message: msg}
	case code == 429:
		return SQLState{Code: "53400", Message: msg}
	case code >= 500 && code < 600:
		return SQLState{Code: "08006", Message: msg}
	}
	return SQLState{Code: "XX000", Message: msg}
}
