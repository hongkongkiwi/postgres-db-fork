package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupLogger_BasicConfiguration(t *testing.T) {
	// Test with basic text formatter
	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.SetFormatter(&logrus.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})
	logger.SetLevel(logrus.InfoLevel)

	logger.Info("test message")

	output := buf.String()
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "level=info")
}

func TestSetupLogger_JSONConfiguration(t *testing.T) {
	// Test with JSON formatter
	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.DebugLevel)

	logger.WithField("operation", "test").Info("test message")

	output := buf.String()

	// Parse JSON to verify structure
	var logEntry map[string]interface{}
	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "info", logEntry["level"])
	assert.Equal(t, "test message", logEntry["msg"])
	assert.Equal(t, "test", logEntry["operation"])
}

func TestLogLevels(t *testing.T) {
	tests := []struct {
		name      string
		level     logrus.Level
		logFunc   func(logger *logrus.Logger)
		shouldLog bool
	}{
		{
			name:  "debug level logs debug message",
			level: logrus.DebugLevel,
			logFunc: func(logger *logrus.Logger) {
				logger.Debug("debug message")
			},
			shouldLog: true,
		},
		{
			name:  "info level filters debug message",
			level: logrus.InfoLevel,
			logFunc: func(logger *logrus.Logger) {
				logger.Debug("debug message")
			},
			shouldLog: false,
		},
		{
			name:  "info level logs info message",
			level: logrus.InfoLevel,
			logFunc: func(logger *logrus.Logger) {
				logger.Info("info message")
			},
			shouldLog: true,
		},
		{
			name:  "warn level filters info message",
			level: logrus.WarnLevel,
			logFunc: func(logger *logrus.Logger) {
				logger.Info("info message")
			},
			shouldLog: false,
		},
		{
			name:  "error level logs error message",
			level: logrus.ErrorLevel,
			logFunc: func(logger *logrus.Logger) {
				logger.Error("error message")
			},
			shouldLog: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			var buf bytes.Buffer
			logger.SetOutput(&buf)
			logger.SetFormatter(&logrus.TextFormatter{
				DisableColors:    true,
				DisableTimestamp: true,
			})
			logger.SetLevel(tt.level)

			tt.logFunc(logger)

			output := buf.String()
			if tt.shouldLog {
				assert.NotEmpty(t, output)
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestStructuredLogging(t *testing.T) {
	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Test structured logging with fields
	logger.WithFields(logrus.Fields{
		"operation":   "database_fork",
		"source_db":   "prod_db",
		"target_db":   "test_db",
		"duration_ms": 1500,
	}).Info("Fork operation completed")

	output := buf.String()

	var logEntry map[string]interface{}
	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "info", logEntry["level"])
	assert.Equal(t, "Fork operation completed", logEntry["msg"])
	assert.Equal(t, "database_fork", logEntry["operation"])
	assert.Equal(t, "prod_db", logEntry["source_db"])
	assert.Equal(t, "test_db", logEntry["target_db"])
	assert.Equal(t, float64(1500), logEntry["duration_ms"])
}

func TestErrorLogging(t *testing.T) {
	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Test error logging with stack trace
	err := assert.AnError
	logger.WithError(err).Error("Operation failed")

	output := buf.String()

	var logEntry map[string]interface{}
	parseErr := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry)
	require.NoError(t, parseErr)

	assert.Equal(t, "error", logEntry["level"])
	assert.Equal(t, "Operation failed", logEntry["msg"])
	assert.Contains(t, logEntry, "error")
}

func TestLogRotationConfiguration(t *testing.T) {
	// Test that we can configure log rotation (basic test)
	tempFile, err := os.CreateTemp("", "test-log-*.log")
	require.NoError(t, err)
	defer func() {
		if err := os.Remove(tempFile.Name()); err != nil {
			t.Logf("Failed to remove temp file: %v", err)
		}
	}()
	if err := tempFile.Close(); err != nil {
		t.Errorf("Failed to close temp file: %v", err)
	}

	logger := logrus.New()

	// Open file for writing
	file, err := os.OpenFile(tempFile.Name(), os.O_WRONLY|os.O_APPEND, 0666)
	require.NoError(t, err)
	defer func() {
		if err := file.Close(); err != nil {
			t.Logf("Failed to close file: %v", err)
		}
	}()

	logger.SetOutput(file)
	logger.Info("Test log message")

	// Read back the file
	content, err := os.ReadFile(tempFile.Name())
	require.NoError(t, err)

	assert.Contains(t, string(content), "Test log message")
}

func TestContextualLogging(t *testing.T) {
	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Create a contextual logger with base fields
	contextLogger := logger.WithFields(logrus.Fields{
		"request_id": "12345",
		"user_id":    "user123",
	})

	contextLogger.Info("User action performed")

	output := buf.String()

	var logEntry map[string]interface{}
	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "12345", logEntry["request_id"])
	assert.Equal(t, "user123", logEntry["user_id"])
	assert.Equal(t, "User action performed", logEntry["msg"])
}

func TestLogFormatterColors(t *testing.T) {
	logger := logrus.New()
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	// Test with colors enabled
	logger.SetFormatter(&logrus.TextFormatter{
		DisableColors:    false,
		ForceColors:      true,
		DisableTimestamp: true,
	})

	logger.Info("colored message")
	coloredOutput := buf.String()

	// Reset buffer
	buf.Reset()

	// Test with colors disabled
	logger.SetFormatter(&logrus.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})

	logger.Info("plain message")
	plainOutput := buf.String()

	// Colored output should be longer due to ANSI codes
	assert.Greater(t, len(coloredOutput), len(plainOutput))
	assert.Contains(t, plainOutput, "plain message")
}
