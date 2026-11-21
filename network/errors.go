package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
)

const (
	ContractVersion     = 1
	ExitCodeBreakerOpen = 75 // EX_TEMPFAIL
	ErrorLinePrefix     = "[kaizen-mangal:error] "
	BreakerOpenMarker   = "[kaizen-mangal:breaker-open]"
)

// StructuredError carries machine-readable failure metadata for Kaizen (RFC 0001 Section 6.F.4).
type StructuredError struct {
	Code            string `json:"code"`
	Status          int    `json:"status,omitempty"`
	Domain          string `json:"domain,omitempty"`
	ContractVersion int    `json:"contract_version"`
	Message         string `json:"-"`
	ExitCode        int    `json:"-"`
	Cause           error  `json:"-"`
}

func (e *StructuredError) Error() string {
	return e.Message
}

func (e *StructuredError) Unwrap() error {
	return e.Cause
}

// StructuredLine returns the single stderr JSON line prefixed with "[kaizen-mangal:error] ".
func (e *StructuredError) StructuredLine() string {
	payload := struct {
		Code            string `json:"code"`
		Status          int    `json:"status,omitempty"`
		Domain          string `json:"domain,omitempty"`
		ContractVersion int    `json:"contract_version"`
	}{
		Code:            e.Code,
		Status:          e.Status,
		Domain:          e.Domain,
		ContractVersion: e.ContractVersion,
	}
	if payload.ContractVersion == 0 {
		payload.ContractVersion = ContractVersion
	}
	b, _ := json.Marshal(payload)
	return ErrorLinePrefix + string(b)
}

// FormatErrorLine formats any error into the single "[kaizen-mangal:error] {json}" line.
func FormatErrorLine(err error) string {
	if err == nil {
		return ""
	}
	var se *StructuredError
	if errors.As(err, &se) {
		return se.StructuredLine()
	}
	return (&StructuredError{
		Code:            classifyErrorCode(err),
		ContractVersion: ContractVersion,
	}).StructuredLine()
}

// ExitCodeFor returns the process exit code for err (75 for breaker-open, 1 otherwise).
func ExitCodeFor(err error) int {
	var se *StructuredError
	if errors.As(err, &se) && se.ExitCode != 0 {
		return se.ExitCode
	}
	return 1
}

// NewBreakerOpenError builds the canonical fast-fail error for an open circuit breaker.
// It strictly omits any Kaizen retryable keyword or HTTP status code (RFC 0001 Section 6.F.4 & Q7).
func NewBreakerOpenError(domain string) *StructuredError {
	return &StructuredError{
		Code:            "breaker_open",
		Domain:          domain,
		ContractVersion: ContractVersion,
		Message:         fmt.Sprintf("%s domain %s is cooling down after repeated challenge failures", BreakerOpenMarker, domain),
		ExitCode:        ExitCodeBreakerOpen,
	}
}

// NormalizeError wraps raw transport errors so transient conditions always include
// Kaizen's expected retryable keywords ("timeout", "connection refused", "HTTP <code>").
func NormalizeError(err error, domain string, statusCode int) error {
	if err == nil {
		return nil
	}
	var existing *StructuredError
	if errors.As(err, &existing) {
		return err
	}

	lower := strings.ToLower(err.Error())

	if isTimeoutError(err, lower) {
		msg := err.Error()
		if !strings.Contains(lower, "timeout") {
			msg = fmt.Sprintf("timeout: %s", msg)
		}
		return &StructuredError{
			Code:            "timeout",
			Domain:          domain,
			ContractVersion: ContractVersion,
			Message:         msg,
			Cause:           err,
		}
	}

	if isConnectionRefusedError(err, lower) {
		msg := err.Error()
		if !strings.Contains(lower, "connection refused") {
			msg = fmt.Sprintf("connection refused: %s", msg)
		}
		return &StructuredError{
			Code:            "connection_refused",
			Domain:          domain,
			ContractVersion: ContractVersion,
			Message:         msg,
			Cause:           err,
		}
	}

	if statusCode >= 400 {
		return &StructuredError{
			Code:            "http_error",
			Status:          statusCode,
			Domain:          domain,
			ContractVersion: ContractVersion,
			Message:         fmt.Sprintf("HTTP %d from %s: %s", statusCode, domain, err.Error()),
			Cause:           err,
		}
	}

	return &StructuredError{
		Code:            classifyErrorCode(err),
		Domain:          domain,
		ContractVersion: ContractVersion,
		Message:         err.Error(),
		Cause:           err,
	}
}

func isTimeoutError(err error, lower string) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "i/o timeout") ||
		strings.Contains(lower, "client.timeout") ||
		strings.Contains(lower, "timeout")
}

func isConnectionRefusedError(err error, lower string) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "actively refused")
}

func classifyErrorCode(err error) string {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "cloudflare"):
		return "cf_challenge"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "timeout"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused"):
		return "connection_refused"
	default:
		return "error"
	}
}
