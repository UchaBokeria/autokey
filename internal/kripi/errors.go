package kripi

import (
	"fmt"
	"time"
)

// Failure classes per the KripiCard 202 contract (spec §7).
type FailureClass int

const (
	// CleanFail: 4xx success:false, nothing charged, safe to retry.
	CleanFail FailureClass = iota
	// PendingRefunded: HTTP 202, no code — already refunded, do NOT retry.
	PendingRefunded
	// RefundPending: HTTP 202 + REFUND_PENDING — charged, support handles, do NOT retry.
	RefundPending
	// RateLimited: 429 — honor retry_after.
	RateLimited
	// OperationInFlight: proxy op running — wait, do not backoff-retry blindly.
	OperationInFlight
)

// CardError is a classified KripiCard failure.
type CardError struct {
	Class      FailureClass
	Message    string
	Scope      string // rate-limit scope: burst|sustained|write|ip
	RetryAfter time.Duration
	Code       string // provider code, e.g. REFUND_PENDING, OPERATION_IN_FLIGHT
}

func (e *CardError) Error() string {
	return fmt.Sprintf("kripicard %s: %s", e.Class, e.Message)
}

// Retryable reports whether the operation may be retried without double-charge risk.
func (e *CardError) Retryable() bool {
	return e.Class == CleanFail
}

func (c FailureClass) String() string {
	switch c {
	case CleanFail:
		return "clean-fail"
	case PendingRefunded:
		return "pending-refunded"
	case RefundPending:
		return "refund-pending"
	case RateLimited:
		return "rate-limited"
	case OperationInFlight:
		return "operation-in-flight"
	default:
		return "unknown"
	}
}

// envelope mirrors the KripiCard response envelope.
type envelope struct {
	Success   bool    `json:"success"`
	Message   string  `json:"message"`
	Pending   bool    `json:"pending"`
	Code      string  `json:"code"`
	Scope     string  `json:"scope"`
	RetrySecs float64 `json:"retry_after_seconds"`
}

// classify maps an HTTP status + envelope to a FailureClass.
func classify(status int, env envelope) *CardError {
	switch {
	case status == 429:
		d := time.Duration(env.RetrySecs * float64(time.Second))
		if d <= 0 {
			d = 30 * time.Second
		}
		return &CardError{Class: RateLimited, Message: env.Message, Scope: env.Scope, RetryAfter: d}
	case env.Code == "OPERATION_IN_FLIGHT":
		return &CardError{Class: OperationInFlight, Message: env.Message, Code: env.Code}
	case env.Pending && env.Code == "REFUND_PENDING":
		return &CardError{Class: RefundPending, Message: env.Message, Code: env.Code}
	case env.Pending:
		return &CardError{Class: PendingRefunded, Message: env.Message}
	default:
		return &CardError{Class: CleanFail, Message: env.Message}
	}
}
