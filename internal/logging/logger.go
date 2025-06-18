package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger wraps logrus with enhanced functionality for postgres-db-fork
type Logger struct {
	*logrus.Logger
	config *Config
}

// Config represents logging configuration
type Config struct {
	Level          string `json:"level"`           // debug, info, warn, error
	Format         string `json:"format"`          // text, json
	Output         string `json:"output"`          // stdout, file, both
	FilePath       string `json:"file_path"`       // path to log file
	MaxSize        int    `json:"max_size"`        // max size in megabytes
	MaxBackups     int    `json:"max_backups"`     // max number of backup files
	MaxAge         int    `json:"max_age"`         // max age in days
	Compress       bool   `json:"compress"`        // compress rotated files
	EnableCaller   bool   `json:"enable_caller"`   // include caller information
	EnableHostname bool   `json:"enable_hostname"` // include hostname
}

// DefaultConfig returns default logging configuration
func DefaultConfig() *Config {
	return &Config{
		Level:          "info",
		Format:         "text",
		Output:         "stdout",
		FilePath:       "logs/postgres-db-fork.log",
		MaxSize:        100, // 100MB
		MaxBackups:     3,
		MaxAge:         28, // 28 days
		Compress:       true,
		EnableCaller:   true,
		EnableHostname: true,
	}
}

// NewLogger creates a new enhanced logger
func NewLogger(config *Config) (*Logger, error) {
	if config == nil {
		config = DefaultConfig()
	}

	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %s: %w", config.Level, err)
	}
	logger.SetLevel(level)

	// Set formatter
	switch strings.ToLower(config.Format) {
	case "json":
		formatter := &logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
		}
		if config.EnableHostname {
			hostname, _ := os.Hostname()
			formatter.FieldMap = logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
			}
			logger.WithField("hostname", hostname)
		}
		logger.SetFormatter(formatter)
	case "text":
		formatter := &logrus.TextFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
			FullTimestamp:   true,
		}
		logger.SetFormatter(formatter)
	default:
		return nil, fmt.Errorf("unsupported log format: %s", config.Format)
	}

	// Set output
	var writers []io.Writer

	switch strings.ToLower(config.Output) {
	case "stdout":
		writers = append(writers, os.Stdout)
	case "file":
		fileWriter, err := createFileWriter(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create file writer: %w", err)
		}
		writers = append(writers, fileWriter)
	case "both":
		writers = append(writers, os.Stdout)
		fileWriter, err := createFileWriter(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create file writer: %w", err)
		}
		writers = append(writers, fileWriter)
	default:
		return nil, fmt.Errorf("unsupported log output: %s", config.Output)
	}

	if len(writers) == 1 {
		logger.SetOutput(writers[0])
	} else {
		logger.SetOutput(io.MultiWriter(writers...))
	}

	// Enable caller reporting if requested
	if config.EnableCaller {
		logger.SetReportCaller(true)
	}

	return &Logger{
		Logger: logger,
		config: config,
	}, nil
}

func createFileWriter(config *Config) (io.Writer, error) {
	// Ensure directory exists
	dir := filepath.Dir(config.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create lumberjack logger for rotation
	return &lumberjack.Logger{
		Filename:   config.FilePath,
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAge,
		Compress:   config.Compress,
	}, nil
}

// SetJobContext adds job-specific context to log entries
func (l *Logger) SetJobContext(jobID string) *logrus.Entry {
	return l.WithFields(logrus.Fields{
		"job_id":    jobID,
		"component": "fork",
	})
}

// SetTableContext adds table-specific context to log entries
func (l *Logger) SetTableContext(jobID, table string) *logrus.Entry {
	return l.WithFields(logrus.Fields{
		"job_id":    jobID,
		"table":     table,
		"component": "transfer",
	})
}

// SetValidationContext adds validation-specific context
func (l *Logger) SetValidationContext() *logrus.Entry {
	return l.WithField("component", "validation")
}

// SetConnectionContext adds connection-specific context
func (l *Logger) SetConnectionContext(host string, database string) *logrus.Entry {
	return l.WithFields(logrus.Fields{
		"host":      host,
		"database":  database,
		"component": "connection",
	})
}

// LogTransferProgress logs transfer progress in a structured way
func (l *Logger) LogTransferProgress(jobID, table string, current, total int64, rate float64) {
	progress := float64(current) / float64(total) * 100
	l.WithFields(logrus.Fields{
		"job_id":    jobID,
		"table":     table,
		"current":   current,
		"total":     total,
		"progress":  fmt.Sprintf("%.1f%%", progress),
		"rate_rps":  fmt.Sprintf("%.0f", rate),
		"component": "progress",
	}).Info("Transfer progress")
}

// LogError logs structured error information
func (l *Logger) LogError(err error, context map[string]interface{}) {
	entry := l.WithError(err)
	if context != nil {
		entry = entry.WithFields(context)
	}
	entry.Error("Operation failed")
}

// LogRetry logs retry attempts with backoff information
func (l *Logger) LogRetry(operation string, attempt int, maxAttempts int, backoff time.Duration, err error) {
	l.WithFields(logrus.Fields{
		"operation":    operation,
		"attempt":      attempt,
		"max_attempts": maxAttempts,
		"backoff":      backoff.String(),
		"component":    "retry",
	}).WithError(err).Warn("Retrying operation")
}

// LogMetrics logs performance metrics
func (l *Logger) LogMetrics(metrics map[string]interface{}) {
	l.WithFields(metrics).WithField("component", "metrics").Info("Performance metrics")
}

// LogAudit logs audit trail information
func (l *Logger) LogAudit(action string, details map[string]interface{}) {
	entry := l.WithFields(logrus.Fields{
		"action":    action,
		"timestamp": time.Now(),
		"component": "audit",
	})

	if details != nil {
		entry = entry.WithFields(details)
	}

	entry.Info("Audit event")
}

// UpdateConfig updates the logger configuration at runtime
func (l *Logger) UpdateConfig(config *Config) error {
	// Set log level
	level, err := logrus.ParseLevel(config.Level)
	if err != nil {
		return fmt.Errorf("invalid log level %s: %w", config.Level, err)
	}
	l.SetLevel(level)

	l.config = config
	return nil
}

// GetConfig returns the current logger configuration
func (l *Logger) GetConfig() *Config {
	return l.config
}

// Close closes any file handles (for cleanup)
func (l *Logger) Close() error {
	// If using file output with lumberjack, we don't need to explicitly close
	// as lumberjack handles this automatically
	return nil
}

// Global logger instance
var globalLogger *Logger

// InitGlobalLogger initializes the global logger
func InitGlobalLogger(config *Config) error {
	logger, err := NewLogger(config)
	if err != nil {
		return err
	}
	globalLogger = logger
	return nil
}

// GetGlobalLogger returns the global logger instance
func GetGlobalLogger() *Logger {
	if globalLogger == nil {
		// Initialize with default config if not set
		config := DefaultConfig()
		logger, _ := NewLogger(config)
		globalLogger = logger
	}
	return globalLogger
}

// Convenience functions for global logger

// Debug logs a debug message
func Debug(args ...interface{}) {
	GetGlobalLogger().Debug(args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	GetGlobalLogger().Debugf(format, args...)
}

// Info logs an info message
func Info(args ...interface{}) {
	GetGlobalLogger().Info(args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	GetGlobalLogger().Infof(format, args...)
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	GetGlobalLogger().Warn(args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	GetGlobalLogger().Warnf(format, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	GetGlobalLogger().Error(args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	GetGlobalLogger().Errorf(format, args...)
}

// Fatal logs a fatal message and exits
func Fatal(args ...interface{}) {
	GetGlobalLogger().Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits
func Fatalf(format string, args ...interface{}) {
	GetGlobalLogger().Fatalf(format, args...)
}

// WithJob returns a logger entry with job context
func WithJob(jobID string) *logrus.Entry {
	return GetGlobalLogger().SetJobContext(jobID)
}

// WithTable returns a logger entry with table context
func WithTable(jobID, table string) *logrus.Entry {
	return GetGlobalLogger().SetTableContext(jobID, table)
}

// WithConnection returns a logger entry with connection context
func WithConnection(host, database string) *logrus.Entry {
	return GetGlobalLogger().SetConnectionContext(host, database)
}
