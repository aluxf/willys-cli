package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
)

type CLIError struct {
	Code, Message string
	Status        int
	Cause         error
}

func (e *CLIError) Error() string { return e.Message }
func (e *CLIError) Unwrap() error { return e.Cause }
func failure(code, message string, status int, cause error) error {
	return &CLIError{code, message, status, cause}
}

// Errors use stderr. Successful or partial result data stays on stdout.
func ReportError(w io.Writer, err error, raw bool) int {
	code, message, status := "command_failed", err.Error(), 1
	var typed *CLIError
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		code, message, status = "cancelled", "Cancelled.", 130
		if err != context.Canceled {
			message = "Cancelled. " + err.Error()
		}
	case errors.As(err, &typed):
		code, message, status = typed.Code, typed.Message, typed.Status
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	case errors.As(err, &network):
		code = "network_error"
	}
	if raw {
		_ = json.NewEncoder(w).Encode(Object{"error": Object{"code": code, "message": message}, "exitCode": status})
	} else {
		if status == 130 {
			fmt.Fprintln(w, message)
		} else {
			fmt.Fprintln(w, "Error:", message)
		}
	}
	return status
}

func JSONRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" {
			return true
		}
	}
	return false
}
