package fork

import (
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// ErrorType represents the category of error
type ErrorType string

const (
	ErrorTypeConnection     ErrorType = "connection"
	ErrorTypePermissions    ErrorType = "permissions"
	ErrorTypeConfiguration  ErrorType = "configuration"
	ErrorTypeResourceLimits ErrorType = "resource_limits"
	ErrorTypeDataIntegrity  ErrorType = "data_integrity"
	ErrorTypeTimeout        ErrorType = "timeout"
	ErrorTypeUnknown        ErrorType = "unknown"
)

// ErrorSeverity represents how critical an error is
type ErrorSeverity string

const (
	SeverityFatal     ErrorSeverity = "fatal"     // Operation cannot continue
	SeverityRetryable ErrorSeverity = "retryable" // Can be retried
	SeverityWarning   ErrorSeverity = "warning"   // Non-critical, can continue
)

// ForkError represents a structured error with context
type ForkError struct {
	Type        ErrorType     `json:"type"`
	Severity    ErrorSeverity `json:"severity"`
	Message     string        `json:"message"`
	Details     string        `json:"details,omitempty"`
	Context     string        `json:"context,omitempty"`
	Timestamp   time.Time     `json:"timestamp"`
	Retryable   bool          `json:"retryable"`
	RetryAfter  time.Duration `json:"retry_after,omitempty"`
	OriginalErr error         `json:"-"`
	Cause       error         `json:"cause,omitempty"`
}

// Error implements the error interface
func (fe *ForkError) Error() string {
	if fe.Context != "" {
		return fmt.Sprintf("[%s] %s: %s (context: %s)", fe.Type, fe.Message, fe.Details, fe.Context)
	}
	return fmt.Sprintf("[%s] %s: %s", fe.Type, fe.Message, fe.Details)
}

// Unwrap implements error unwrapping
func (fe *ForkError) Unwrap() error {
	return fe.OriginalErr
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts     int           `json:"max_attempts"`
	InitialDelay    time.Duration `json:"initial_delay"`
	MaxDelay        time.Duration `json:"max_delay"`
	BackoffFactor   float64       `json:"backoff_factor"`
	RetryableErrors []ErrorType   `json:"retryable_errors"`
}

// DefaultRetryConfig returns sensible defaults for CI/CD environments
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		RetryableErrors: []ErrorType{
			ErrorTypeConnection,
			ErrorTypeTimeout,
			ErrorTypeResourceLimits,
		},
	}
}

// ErrorHandler provides structured error handling with retry logic
type ErrorHandler struct {
	config     RetryConfig
	context    string
	logger     *logrus.Logger
	errorCount map[ErrorType]int
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(config RetryConfig, context string) *ErrorHandler {
	return &ErrorHandler{
		config:     config,
		context:    context,
		logger:     logrus.StandardLogger(),
		errorCount: make(map[ErrorType]int),
	}
}

// WrapError wraps an error with additional context and stack trace
func (eh *ErrorHandler) WrapError(err error, message string) error {
	if err == nil {
		return nil
	}

	// If it's already a ForkError, just wrap it
	if fe, ok := err.(*ForkError); ok {
		wrappedErr := errors.Wrap(err, message)
		return &ForkError{
			Type:        fe.Type,
			Severity:    fe.Severity,
			Message:     fmt.Sprintf("%s: %s", message, fe.Message),
			Details:     fe.Details,
			Context:     fe.Context,
			Timestamp:   time.Now(),
			Retryable:   fe.Retryable,
			RetryAfter:  fe.RetryAfter,
			OriginalErr: fe.OriginalErr,
			Cause:       wrappedErr,
		}
	}

	// Classify the error
	errorType, severity, retryable, retryAfter := eh.classifyError(err)
	eh.errorCount[errorType]++

	// Wrap with pkg/errors for stack trace
	wrappedErr := errors.Wrap(err, message)

	return &ForkError{
		Type:        errorType,
		Severity:    severity,
		Message:     message,
		Details:     err.Error(),
		Context:     eh.context,
		Timestamp:   time.Now(),
		Retryable:   retryable,
		RetryAfter:  retryAfter,
		OriginalErr: err,
		Cause:       wrappedErr,
	}
}

// WrapErrorWithContext wraps an error with additional context string
func (eh *ErrorHandler) WrapErrorWithContext(err error, message string, context string) error {
	if err == nil {
		return nil
	}

	// If it's already a ForkError, just wrap it
	if fe, ok := err.(*ForkError); ok {
		wrappedErr := errors.Wrap(err, message)
		contextStr := fe.Context
		if context != "" {
			if contextStr != "" {
				contextStr = fmt.Sprintf("%s; %s", contextStr, context)
			} else {
				contextStr = context
			}
		}

		return &ForkError{
			Type:        fe.Type,
			Severity:    fe.Severity,
			Message:     fmt.Sprintf("%s: %s", message, fe.Message),
			Details:     fe.Details,
			Context:     contextStr,
			Timestamp:   time.Now(),
			Retryable:   fe.Retryable,
			RetryAfter:  fe.RetryAfter,
			OriginalErr: fe.OriginalErr,
			Cause:       wrappedErr,
		}
	}

	// Classify the error
	errorType, severity, retryable, retryAfter := eh.classifyError(err)
	eh.errorCount[errorType]++

	// Wrap with pkg/errors for stack trace
	wrappedErr := errors.Wrap(err, message)

	contextStr := eh.context
	if context != "" {
		if contextStr != "" {
			contextStr = fmt.Sprintf("%s; %s", contextStr, context)
		} else {
			contextStr = context
		}
	}

	return &ForkError{
		Type:        errorType,
		Severity:    severity,
		Message:     message,
		Details:     err.Error(),
		Context:     contextStr,
		Timestamp:   time.Now(),
		Retryable:   retryable,
		RetryAfter:  retryAfter,
		OriginalErr: err,
		Cause:       wrappedErr,
	}
}

// GetRootCause extracts the root cause from a wrapped error
func GetRootCause(err error) error {
	return errors.Cause(err)
}

// GetStackTrace extracts stack trace from error if available
func GetStackTrace(err error) string {
	type stackTracer interface {
		StackTrace() errors.StackTrace
	}

	if st, ok := err.(stackTracer); ok {
		return fmt.Sprintf("%+v", st.StackTrace())
	}
	return ""
}

// classifyError determines the error type, severity, and retry characteristics
func (eh *ErrorHandler) classifyError(err error) (ErrorType, ErrorSeverity, bool, time.Duration) {
	errStr := strings.ToLower(err.Error())

	// PostgreSQL specific errors
	if pqErr, ok := err.(*pq.Error); ok {
		return eh.classifyPostgreSQLError(pqErr)
	}

	// Connection errors
	if strings.Contains(errStr, "connection") {
		if strings.Contains(errStr, "refused") || strings.Contains(errStr, "timeout") {
			return ErrorTypeConnection, SeverityRetryable, true, 5 * time.Second
		}
		return ErrorTypeConnection, SeverityFatal, false, 0
	}

	// Permission errors
	if strings.Contains(errStr, "permission") || strings.Contains(errStr, "access") || strings.Contains(errStr, "denied") {
		return ErrorTypePermissions, SeverityFatal, false, 0
	}

	// Timeout errors
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
		return ErrorTypeTimeout, SeverityRetryable, true, 10 * time.Second
	}

	// Resource limit errors
	if strings.Contains(errStr, "out of memory") || strings.Contains(errStr, "disk full") || strings.Contains(errStr, "too many") {
		return ErrorTypeResourceLimits, SeverityRetryable, true, 30 * time.Second
	}

	// Configuration errors
	if strings.Contains(errStr, "invalid") || strings.Contains(errStr, "config") || strings.Contains(errStr, "parse") {
		return ErrorTypeConfiguration, SeverityFatal, false, 0
	}

	return ErrorTypeUnknown, SeverityFatal, false, 0
}

// classifyPostgreSQLError classifies PostgreSQL-specific errors
func (eh *ErrorHandler) classifyPostgreSQLError(pqErr *pq.Error) (ErrorType, ErrorSeverity, bool, time.Duration) {
	switch pqErr.Code {
	// Connection errors
	case "08000", "08003", "08006":
		return ErrorTypeConnection, SeverityRetryable, true, 5 * time.Second

	// Permission errors
	case "42501", "28000", "28P01":
		return ErrorTypePermissions, SeverityFatal, false, 0

	// Resource errors
	case "53000", "53100", "53200", "53300":
		return ErrorTypeResourceLimits, SeverityRetryable, true, 30 * time.Second

	// Data integrity errors
	case "23000", "23001", "23502", "23503", "23505", "23514":
		return ErrorTypeDataIntegrity, SeverityFatal, false, 0

	// Lock timeout
	case "55P03":
		return ErrorTypeTimeout, SeverityRetryable, true, 10 * time.Second

	// Disk full
	case "58030":
		return ErrorTypeResourceLimits, SeverityRetryable, true, 60 * time.Second

	default:
		// For unknown PostgreSQL errors, be conservative
		return ErrorTypeUnknown, SeverityFatal, false, 0
	}
}

// ShouldRetry determines if an operation should be retried
func (eh *ErrorHandler) ShouldRetry(err error, attempt int) (bool, time.Duration) {
	forkErr, ok := err.(*ForkError)
	if !ok {
		return false, 0
	}

	// Check if we've exceeded max attempts
	if attempt >= eh.config.MaxAttempts {
		return false, 0
	}

	// Check if error is retryable
	if !forkErr.Retryable {
		return false, 0
	}

	// Check if error type is in retryable list
	retryable := false
	for _, retryableType := range eh.config.RetryableErrors {
		if forkErr.Type == retryableType {
			retryable = true
			break
		}
	}

	if !retryable {
		return false, 0
	}

	// Calculate backoff delay
	delay := eh.calculateBackoff(attempt, forkErr.RetryAfter)
	return true, delay
}

// calculateBackoff calculates the backoff delay
func (eh *ErrorHandler) calculateBackoff(attempt int, suggestedDelay time.Duration) time.Duration {
	// Use suggested delay if provided
	if suggestedDelay > 0 {
		return suggestedDelay
	}

	// Exponential backoff
	delay := time.Duration(float64(eh.config.InitialDelay) * (eh.config.BackoffFactor * float64(attempt)))

	if delay > eh.config.MaxDelay {
		delay = eh.config.MaxDelay
	}

	return delay
}

// RetryWithBackoff executes a function with retry logic
func (eh *ErrorHandler) RetryWithBackoff(operation func() error, operationName string) error {
	var lastErr error

	for attempt := 0; attempt < eh.config.MaxAttempts; attempt++ {
		err := operation()
		if err == nil {
			if attempt > 0 {
				eh.logger.Infof("Operation '%s' succeeded after %d attempts", operationName, attempt+1)
			}
			return nil
		}

		// Wrap error if needed
		forkErr := eh.WrapError(err, fmt.Sprintf("Operation '%s' failed", operationName))
		lastErr = forkErr

		// Check if we should retry
		shouldRetry, delay := eh.ShouldRetry(forkErr, attempt)
		if !shouldRetry {
			break
		}

		if attempt < eh.config.MaxAttempts-1 {
			eh.logger.Warnf("Operation '%s' failed (attempt %d/%d), retrying in %v: %v",
				operationName, attempt+1, eh.config.MaxAttempts, delay, err)
			time.Sleep(delay)
		}
	}

	return eh.WrapError(lastErr, fmt.Sprintf("Operation '%s' failed after %d attempts", operationName, eh.config.MaxAttempts))
}

// GetErrorSummary returns a summary of errors encountered
func (eh *ErrorHandler) GetErrorSummary() map[ErrorType]int {
	summary := make(map[ErrorType]int)
	for errorType, count := range eh.errorCount {
		summary[errorType] = count
	}
	return summary
}

// RecoverableError creates a retryable error
func RecoverableError(errorType ErrorType, message string, retryAfter time.Duration) *ForkError {
	return &ForkError{
		Type:       errorType,
		Severity:   SeverityRetryable,
		Message:    message,
		Timestamp:  time.Now(),
		Retryable:  true,
		RetryAfter: retryAfter,
	}
}

// FatalError creates a non-retryable error
func FatalError(errorType ErrorType, message string, details string) *ForkError {
	return &ForkError{
		Type:      errorType,
		Severity:  SeverityFatal,
		Message:   message,
		Details:   details,
		Timestamp: time.Now(),
		Retryable: false,
	}
}

// WarningError creates a warning-level error
func WarningError(errorType ErrorType, message string, details string) *ForkError {
	return &ForkError{
		Type:      errorType,
		Severity:  SeverityWarning,
		Message:   message,
		Details:   details,
		Timestamp: time.Now(),
		Retryable: false,
	}
}

// RetryWithExponentialBackoff retries an operation with exponential backoff
func (eh *ErrorHandler) RetryWithExponentialBackoff(operation func() error, operationName string) error {
	// Create exponential backoff configuration
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = eh.config.InitialDelay
	b.MaxInterval = eh.config.MaxDelay
	b.Multiplier = eh.config.BackoffFactor
	b.MaxElapsedTime = time.Duration(eh.config.MaxAttempts) * eh.config.MaxDelay

	// Add jitter to prevent thundering herd
	b.RandomizationFactor = 0.1

	var lastErr error
	attempt := 0

	retryableOperation := func() error {
		attempt++

		eh.logger.WithFields(logrus.Fields{
			"operation": operationName,
			"attempt":   attempt,
			"context":   eh.context,
		}).Debug("Attempting operation")

		err := operation()
		if err == nil {
			return nil
		}

		// Wrap the error for better context
		wrappedErr := eh.WrapError(err, fmt.Sprintf("Operation '%s' failed on attempt %d", operationName, attempt))
		lastErr = wrappedErr

		// Check if error is retryable
		if forkErr, ok := wrappedErr.(*ForkError); ok {
			if !forkErr.Retryable {
				// Non-retryable error - stop immediately
				return backoff.Permanent(wrappedErr)
			}

			// Log retryable error
			eh.logger.WithFields(logrus.Fields{
				"operation":   operationName,
				"attempt":     attempt,
				"error_type":  forkErr.Type,
				"retry_after": forkErr.RetryAfter,
			}).Warn("Operation failed, will retry")

			return wrappedErr
		}

		// Unknown error type - treat as retryable but log warning
		eh.logger.WithFields(logrus.Fields{
			"operation": operationName,
			"attempt":   attempt,
			"error":     err.Error(),
		}).Warn("Unknown error type, treating as retryable")

		return wrappedErr
	}

	// Execute with retry
	if err := backoff.Retry(retryableOperation, b); err != nil {
		// Final failure after all retries
		finalErr := eh.WrapError(lastErr, fmt.Sprintf("Operation '%s' failed after %d attempts", operationName, attempt))

		eh.logger.WithFields(logrus.Fields{
			"operation":      operationName,
			"total_attempts": attempt,
			"final_error":    err.Error(),
		}).Error("Operation failed permanently")

		return finalErr
	}

	// Success
	eh.logger.WithFields(logrus.Fields{
		"operation": operationName,
		"attempts":  attempt,
	}).Info("Operation succeeded")

	return nil
}

// RetryWithCircuitBreaker implements circuit breaker pattern for database operations
func (eh *ErrorHandler) RetryWithCircuitBreaker(operation func() error, operationName string) error {
	// Simple circuit breaker implementation
	const (
		circuitBreakerThreshold = 5                // Number of consecutive failures before opening
		circuitBreakerTimeout   = 30 * time.Second // Time to wait before trying again
	)

	// This is a simplified implementation - in production you might want to use
	// a more sophisticated circuit breaker library like github.com/sony/gobreaker

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = eh.config.InitialDelay
	b.MaxInterval = eh.config.MaxDelay
	b.Multiplier = eh.config.BackoffFactor
	b.MaxElapsedTime = circuitBreakerTimeout

	consecutiveFailures := 0
	var lastErr error

	retryableOperation := func() error {
		err := operation()
		if err == nil {
			consecutiveFailures = 0 // Reset on success
			return nil
		}

		consecutiveFailures++
		lastErr = eh.WrapError(err, fmt.Sprintf("Circuit breaker: %s", operationName))

		// Check if we should open the circuit
		if consecutiveFailures >= circuitBreakerThreshold {
			eh.logger.WithFields(logrus.Fields{
				"operation":            operationName,
				"consecutive_failures": consecutiveFailures,
				"threshold":            circuitBreakerThreshold,
			}).Error("Circuit breaker opened - too many consecutive failures")

			return backoff.Permanent(eh.WrapError(lastErr, "Circuit breaker opened"))
		}

		return lastErr
	}

	return backoff.Retry(retryableOperation, b)
}

// RetryWithCustomBackoff allows for custom backoff strategies
func (eh *ErrorHandler) RetryWithCustomBackoff(operation func() error, operationName string, backoffStrategy backoff.BackOff) error {
	var lastErr error
	attempt := 0

	retryableOperation := func() error {
		attempt++
		err := operation()
		if err == nil {
			return nil
		}

		wrappedErr := eh.WrapError(err, fmt.Sprintf("%s (attempt %d)", operationName, attempt))
		lastErr = wrappedErr

		// Check if error is retryable
		if forkErr, ok := wrappedErr.(*ForkError); ok && !forkErr.Retryable {
			return backoff.Permanent(wrappedErr)
		}

		return wrappedErr
	}

	if err := backoff.Retry(retryableOperation, backoffStrategy); err != nil {
		return eh.WrapError(lastErr, fmt.Sprintf("%s failed permanently after %d attempts", operationName, attempt))
	}

	return nil
}
