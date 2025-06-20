package fork

import (
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkError_Error(t *testing.T) {
	tests := []struct {
		name     string
		forkErr  *ForkError
		expected string
	}{
		{
			name: "error with context",
			forkErr: &ForkError{
				Type:    ErrorTypeConnection,
				Message: "Connection failed",
				Details: "timeout occurred",
				Context: "connecting to source database",
			},
			expected: "[connection] Connection failed: timeout occurred (context: connecting to source database)",
		},
		{
			name: "error without context",
			forkErr: &ForkError{
				Type:    ErrorTypePermissions,
				Message: "Access denied",
				Details: "insufficient privileges",
			},
			expected: "[permissions] Access denied: insufficient privileges",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.forkErr.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestForkError_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	forkErr := &ForkError{
		Type:        ErrorTypeConnection,
		Message:     "Wrapped error",
		OriginalErr: originalErr,
	}

	unwrapped := forkErr.Unwrap()
	assert.Equal(t, originalErr, unwrapped)
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	assert.Equal(t, 3, config.MaxAttempts)
	assert.Equal(t, time.Second, config.InitialDelay)
	assert.Equal(t, 30*time.Second, config.MaxDelay)
	assert.Equal(t, 2.0, config.BackoffFactor)
	assert.Contains(t, config.RetryableErrors, ErrorTypeConnection)
	assert.Contains(t, config.RetryableErrors, ErrorTypeTimeout)
	assert.Contains(t, config.RetryableErrors, ErrorTypeResourceLimits)
}

func TestNewErrorHandler(t *testing.T) {
	config := DefaultRetryConfig()
	context := "test operation"

	handler := NewErrorHandler(config, context)

	assert.Equal(t, config, handler.config)
	assert.Equal(t, context, handler.context)
	assert.NotNil(t, handler.logger)
	assert.NotNil(t, handler.errorCount)
}

func TestErrorHandler_WrapError(t *testing.T) {
	handler := NewErrorHandler(DefaultRetryConfig(), "test context")

	tests := []struct {
		name          string
		err           error
		message       string
		context       string
		expectedType  ErrorType
		expectedRetry bool
		expectNil     bool
	}{
		{
			name:      "nil error",
			err:       nil,
			message:   "test message",
			context:   "test context",
			expectNil: true,
		},
		{
			name:          "connection refused error",
			err:           errors.New("connection refused"),
			message:       "Failed to connect",
			context:       "database connection",
			expectedType:  ErrorTypeConnection,
			expectedRetry: true,
		},
		{
			name:          "permission denied error",
			err:           errors.New("permission denied"),
			message:       "Access failed",
			context:       "database access",
			expectedType:  ErrorTypePermissions,
			expectedRetry: false,
		},
		{
			name:          "timeout error",
			err:           errors.New("timeout exceeded"),
			message:       "Operation timed out",
			context:       "query execution",
			expectedType:  ErrorTypeTimeout,
			expectedRetry: true,
		},
		{
			name:          "existing ForkError",
			err:           &ForkError{Type: ErrorTypeConfiguration, Message: "Config error"},
			message:       "Wrapped config error",
			context:       "new context",
			expectedType:  ErrorTypeConfiguration,
			expectedRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result error
			if tt.context != "" {
				result = handler.WrapErrorWithContext(tt.err, tt.message, tt.context)
			} else {
				result = handler.WrapError(tt.err, tt.message)
			}

			if tt.expectNil {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			forkErr, ok := result.(*ForkError)
			require.True(t, ok, "Expected ForkError but got %T", result)
			assert.Equal(t, tt.expectedType, forkErr.Type)
			assert.Equal(t, tt.expectedRetry, forkErr.Retryable)
			if tt.context != "" {
				assert.Contains(t, forkErr.Context, tt.context)
			}
		})
	}
}

func TestErrorHandler_classifyError(t *testing.T) {
	handler := NewErrorHandler(DefaultRetryConfig(), "test")

	tests := []struct {
		name             string
		err              error
		expectedType     ErrorType
		expectedSeverity ErrorSeverity
		expectedRetry    bool
	}{
		{
			name:             "connection refused",
			err:              errors.New("connection refused"),
			expectedType:     ErrorTypeConnection,
			expectedSeverity: SeverityRetryable,
			expectedRetry:    true,
		},
		{
			name:             "permission denied",
			err:              errors.New("permission denied"),
			expectedType:     ErrorTypePermissions,
			expectedSeverity: SeverityFatal,
			expectedRetry:    false,
		},
		{
			name:             "timeout error",
			err:              errors.New("timeout occurred"),
			expectedType:     ErrorTypeTimeout,
			expectedSeverity: SeverityRetryable,
			expectedRetry:    true,
		},
		{
			name:             "out of memory",
			err:              errors.New("out of memory"),
			expectedType:     ErrorTypeResourceLimits,
			expectedSeverity: SeverityRetryable,
			expectedRetry:    true,
		},
		{
			name:             "invalid configuration",
			err:              errors.New("invalid config"),
			expectedType:     ErrorTypeConfiguration,
			expectedSeverity: SeverityFatal,
			expectedRetry:    false,
		},
		{
			name:             "unknown error",
			err:              errors.New("some random error"),
			expectedType:     ErrorTypeUnknown,
			expectedSeverity: SeverityFatal,
			expectedRetry:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorType, severity, retryable, _ := handler.classifyError(tt.err)
			assert.Equal(t, tt.expectedType, errorType)
			assert.Equal(t, tt.expectedSeverity, severity)
			assert.Equal(t, tt.expectedRetry, retryable)
		})
	}
}

func TestErrorHandler_classifyPostgreSQLError(t *testing.T) {
	handler := NewErrorHandler(DefaultRetryConfig(), "test")

	tests := []struct {
		name             string
		pqError          *pq.Error
		expectedType     ErrorType
		expectedSeverity ErrorSeverity
		expectedRetry    bool
	}{
		{
			name:             "connection failure",
			pqError:          &pq.Error{Code: "08006"}, // connection_failure
			expectedType:     ErrorTypeConnection,
			expectedSeverity: SeverityRetryable,
			expectedRetry:    true,
		},
		{
			name:             "insufficient privilege",
			pqError:          &pq.Error{Code: "42501"}, // insufficient_privilege
			expectedType:     ErrorTypePermissions,
			expectedSeverity: SeverityFatal,
			expectedRetry:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorType, severity, retryable, _ := handler.classifyPostgreSQLError(tt.pqError)
			assert.Equal(t, tt.expectedType, errorType)
			assert.Equal(t, tt.expectedSeverity, severity)
			assert.Equal(t, tt.expectedRetry, retryable)
		})
	}
}

func TestErrorHandler_ShouldRetry(t *testing.T) {
	config := DefaultRetryConfig()
	handler := NewErrorHandler(config, "test")

	tests := []struct {
		name        string
		err         error
		attempt     int
		shouldRetry bool
		expectDelay bool
	}{
		{
			name: "retryable error within max attempts",
			err: &ForkError{
				Type:      ErrorTypeConnection,
				Retryable: true,
			},
			attempt:     1,
			shouldRetry: true,
			expectDelay: true,
		},
		{
			name: "retryable error at max attempts",
			err: &ForkError{
				Type:      ErrorTypeConnection,
				Retryable: true,
			},
			attempt:     3,
			shouldRetry: false,
			expectDelay: false,
		},
		{
			name: "non-retryable error",
			err: &ForkError{
				Type:      ErrorTypePermissions,
				Retryable: false,
			},
			attempt:     1,
			shouldRetry: false,
			expectDelay: false,
		},
		{
			name:        "non-ForkError",
			err:         errors.New("regular error"),
			attempt:     1,
			shouldRetry: false,
			expectDelay: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldRetry, delay := handler.ShouldRetry(tt.err, tt.attempt)
			assert.Equal(t, tt.shouldRetry, shouldRetry)

			if tt.expectDelay {
				assert.Greater(t, delay, time.Duration(0))
			} else {
				assert.Equal(t, time.Duration(0), delay)
			}
		})
	}
}

func TestErrorHandler_calculateBackoff(t *testing.T) {
	config := RetryConfig{
		InitialDelay:  time.Second,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
	}
	handler := NewErrorHandler(config, "test")

	tests := []struct {
		name           string
		attempt        int
		suggestedDelay time.Duration
		expectedMin    time.Duration
		expectedMax    time.Duration
	}{
		{
			name:           "first attempt",
			attempt:        1,
			suggestedDelay: 0,
			expectedMin:    2 * time.Second, // 1s * (2.0 * 1) = 2s
			expectedMax:    2 * time.Second,
		},
		{
			name:           "second attempt",
			attempt:        2,
			suggestedDelay: 0,
			expectedMin:    4 * time.Second, // 1s * (2.0 * 2) = 4s
			expectedMax:    4 * time.Second,
		},
		{
			name:           "capped at max delay",
			attempt:        10,
			suggestedDelay: 0,
			expectedMin:    10 * time.Second,
			expectedMax:    10 * time.Second,
		},
		{
			name:           "with suggested delay",
			attempt:        1,
			suggestedDelay: 3 * time.Second,
			expectedMin:    3 * time.Second,
			expectedMax:    3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := handler.calculateBackoff(tt.attempt, tt.suggestedDelay)
			assert.GreaterOrEqual(t, delay, tt.expectedMin)
			assert.LessOrEqual(t, delay, tt.expectedMax)
		})
	}
}

func TestErrorHandler_GetErrorSummary(t *testing.T) {
	handler := NewErrorHandler(DefaultRetryConfig(), "test")

	// Simulate some errors
	_ = handler.WrapErrorWithContext(errors.New("connection refused"), "msg1", "ctx1")
	_ = handler.WrapErrorWithContext(errors.New("connection timeout"), "msg2", "ctx2")
	_ = handler.WrapErrorWithContext(errors.New("permission denied"), "msg3", "ctx3")

	summary := handler.GetErrorSummary()

	assert.Equal(t, 2, summary[ErrorTypeConnection])
	assert.Equal(t, 1, summary[ErrorTypePermissions])
}

func TestRecoverableError(t *testing.T) {
	err := RecoverableError(ErrorTypeConnection, "Connection failed", 5*time.Second)

	assert.Equal(t, ErrorTypeConnection, err.Type)
	assert.Equal(t, SeverityRetryable, err.Severity)
	assert.Equal(t, "Connection failed", err.Message)
	assert.True(t, err.Retryable)
	assert.Equal(t, 5*time.Second, err.RetryAfter)
}

func TestFatalError(t *testing.T) {
	err := FatalError(ErrorTypePermissions, "Access denied", "User lacks privileges")

	assert.Equal(t, ErrorTypePermissions, err.Type)
	assert.Equal(t, SeverityFatal, err.Severity)
	assert.Equal(t, "Access denied", err.Message)
	assert.Equal(t, "User lacks privileges", err.Details)
	assert.False(t, err.Retryable)
}

func TestWarningError(t *testing.T) {
	err := WarningError(ErrorTypeDataIntegrity, "Data inconsistency", "Some rows skipped")

	assert.Equal(t, ErrorTypeDataIntegrity, err.Type)
	assert.Equal(t, SeverityWarning, err.Severity)
	assert.Equal(t, "Data inconsistency", err.Message)
	assert.Equal(t, "Some rows skipped", err.Details)
	assert.False(t, err.Retryable)
}
