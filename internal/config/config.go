package config

import (
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-playground/validator/v10"
)

// DatabaseConfig represents a PostgreSQL database connection configuration
type DatabaseConfig struct {
	// URI takes precedence over individual parameters if provided
	URI      string `mapstructure:"uri" yaml:"uri" validate:"omitempty,uri"`
	Host     string `mapstructure:"host" yaml:"host" validate:"required_without=URI,hostname_rfc1123|ip"`
	Port     int    `mapstructure:"port" yaml:"port" validate:"required_without=URI,min=1,max=65535"`
	Username string `mapstructure:"username" yaml:"username" validate:"required_without=URI,min=1"`
	Password string `mapstructure:"password" yaml:"password" validate:"omitempty"`
	Database string `mapstructure:"database" yaml:"database" validate:"required,min=1,max=63"`
	SSLMode  string `mapstructure:"sslmode" yaml:"sslmode" validate:"omitempty,oneof=disable allow prefer require verify-ca verify-full"`
}

// ForkConfig represents the complete configuration for a database fork operation
type ForkConfig struct {
	Source         DatabaseConfig `mapstructure:"source" yaml:"source" validate:"required"`
	Destination    DatabaseConfig `mapstructure:"destination" yaml:"destination" validate:"required"`
	TargetDatabase string         `mapstructure:"target_database" yaml:"target_database" validate:"required,min=1,max=63"`

	// Fork options
	DropIfExists   bool          `mapstructure:"drop_if_exists" yaml:"drop_if_exists"`
	MaxConnections int           `mapstructure:"max_connections" yaml:"max_connections" validate:"min=1,max=100"`
	ChunkSize      int           `mapstructure:"chunk_size" yaml:"chunk_size" validate:"min=100,max=100000"`
	Timeout        time.Duration `mapstructure:"timeout" yaml:"timeout" validate:"min=1m,max=24h"`
	SchemaOnly     bool          `mapstructure:"schema_only" yaml:"schema_only"`
	DataOnly       bool          `mapstructure:"data_only" yaml:"data_only"`

	// Table filtering
	IncludeTables []string `mapstructure:"include_tables" yaml:"include_tables" validate:"dive,min=1"`
	ExcludeTables []string `mapstructure:"exclude_tables" yaml:"exclude_tables" validate:"dive,min=1"`

	// CI/CD Integration features
	OutputFormat string `mapstructure:"output_format" yaml:"output_format" validate:"oneof=text json"`
	Quiet        bool   `mapstructure:"quiet" yaml:"quiet"`
	DryRun       bool   `mapstructure:"dry_run" yaml:"dry_run"`
	LogLevel     string `mapstructure:"log_level" yaml:"log_level" validate:"oneof=debug info warn error"`

	// Template variables for dynamic naming
	TemplateVars map[string]string `mapstructure:"template_vars" yaml:"template_vars"`

	// Hooks for custom actions
	Hooks HooksConfig `mapstructure:"hooks" yaml:"hooks"`
}

// HooksConfig defines custom scripts or commands to be executed at different stages
type HooksConfig struct {
	// PreFork commands are executed before the fork operation begins
	PreFork []string `mapstructure:"pre_fork" yaml:"pre_fork"`

	// PostFork commands are executed after a successful fork operation
	PostFork []string `mapstructure:"post_fork" yaml:"post_fork"`

	// OnError commands are executed if the fork operation fails
	OnError []string `mapstructure:"on_error" yaml:"on_error"`
}

// OutputConfig represents the output configuration for CI/CD integration
type OutputConfig struct {
	Format   string `json:"format"`
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
	Database string `json:"database,omitempty"`
	Duration string `json:"duration,omitempty"`
	Size     string `json:"size,omitempty"`
}

// Global validator instance
var validate *validator.Validate

func init() {
	validate = validator.New()

	// Register custom validators
	if err := validate.RegisterValidation("required_without", requiredWithoutValidator); err != nil {
		panic(fmt.Sprintf("failed to register custom validation: %v", err))
	}
}

// requiredWithoutValidator is a custom validator that requires a field when another field is empty
func requiredWithoutValidator(fl validator.FieldLevel) bool {
	param := fl.Param()
	field := fl.Parent().FieldByName(param)

	if !field.IsValid() {
		return false
	}

	// If the parameter field is empty, this field is required
	if field.Kind() == reflect.String && field.String() == "" {
		return fl.Field().String() != ""
	}

	return true
}

// parsePostgreSQLURI parses a PostgreSQL URI using the standard library's url.Parse
func parsePostgreSQLURI(uri string) (*DatabaseConfig, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL URI: %w", err)
	}

	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("invalid PostgreSQL URI scheme: %s", u.Scheme)
	}

	config := &DatabaseConfig{
		URI: uri,
	}

	if u.User != nil {
		config.Username = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			config.Password = pass // pragma: allowlist secret
		}
	}

	config.Host = u.Hostname()

	if portStr := u.Port(); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		} else {
			return nil, fmt.Errorf("invalid port in URI: %q", portStr)
		}
	} else {
		config.Port = 5432 // Default PostgreSQL port
	}

	if u.Path != "" {
		config.Database = strings.TrimPrefix(u.Path, "/")
	}

	q := u.Query()
	if sslmode := q.Get("sslmode"); sslmode != "" {
		config.SSLMode = sslmode
	}

	return config, nil
}

// ConnectionString builds a PostgreSQL connection string from the config
func (c *DatabaseConfig) ConnectionString() string {
	// If URI is provided, use it directly (pq library supports URIs natively)
	if c.URI != "" {
		return c.URI
	}

	// Fall back to building connection string from individual parameters
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}

	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", // pragma: allowlist secret
		c.Host, c.Port, c.Username, c.Password, c.Database, sslMode,
	)
}

// IsSameServer checks if source and destination are on the same PostgreSQL server
func (c *ForkConfig) IsSameServer() bool {
	return c.Source.Host == c.Destination.Host &&
		c.Source.Port == c.Destination.Port &&
		c.Source.Username == c.Destination.Username
}

// LoadFromEnvironment loads configuration from environment variables with PGFORK_ prefix
func (c *ForkConfig) LoadFromEnvironment() {
	// Source configuration - check for URI first
	if sourceURI := os.Getenv("PGFORK_SOURCE_URI"); sourceURI != "" {
		if parsedConfig, err := parsePostgreSQLURI(sourceURI); err == nil {
			c.Source = *parsedConfig
		}
		// If URI parsing fails, fall back to individual parameters
	}

	// If no URI or URI parsing failed, load individual source parameters
	if c.Source.URI == "" {
		if host := os.Getenv("PGFORK_SOURCE_HOST"); host != "" {
			c.Source.Host = host
		}
		if port := os.Getenv("PGFORK_SOURCE_PORT"); port != "" {
			if p, err := strconv.Atoi(port); err == nil {
				c.Source.Port = p
			}
		}
		if user := os.Getenv("PGFORK_SOURCE_USER"); user != "" {
			c.Source.Username = user
		}
		if password := os.Getenv("PGFORK_SOURCE_PASSWORD"); password != "" {
			c.Source.Password = password
		}
		if database := os.Getenv("PGFORK_SOURCE_DATABASE"); database != "" {
			c.Source.Database = database
		}
		if sslmode := os.Getenv("PGFORK_SOURCE_SSLMODE"); sslmode != "" {
			c.Source.SSLMode = sslmode
		}
	}

	// Destination configuration - check for URI first
	if destURI := os.Getenv("PGFORK_DEST_URI"); destURI != "" {
		if parsedConfig, err := parsePostgreSQLURI(destURI); err == nil {
			c.Destination = *parsedConfig
		}
		// If URI parsing fails, fall back to individual parameters
	} else if targetURI := os.Getenv("PGFORK_TARGET_URI"); targetURI != "" {
		// Support both PGFORK_DEST_URI and PGFORK_TARGET_URI for compatibility
		if parsedConfig, err := parsePostgreSQLURI(targetURI); err == nil {
			c.Destination = *parsedConfig
		}
	}

	// If no URI or URI parsing failed, load individual destination parameters
	if c.Destination.URI == "" {
		if host := os.Getenv("PGFORK_DEST_HOST"); host != "" {
			c.Destination.Host = host
		}
		if port := os.Getenv("PGFORK_DEST_PORT"); port != "" {
			if p, err := strconv.Atoi(port); err == nil {
				c.Destination.Port = p
			}
		}
		if user := os.Getenv("PGFORK_DEST_USER"); user != "" {
			c.Destination.Username = user
		}
		if password := os.Getenv("PGFORK_DEST_PASSWORD"); password != "" {
			c.Destination.Password = password
		}
		if sslmode := os.Getenv("PGFORK_DEST_SSLMODE"); sslmode != "" {
			c.Destination.SSLMode = sslmode
		}
	}

	// Fork configuration
	if targetDB := os.Getenv("PGFORK_TARGET_DATABASE"); targetDB != "" {
		c.TargetDatabase = targetDB
	}
	if dropExists := os.Getenv("PGFORK_DROP_IF_EXISTS"); dropExists != "" {
		c.DropIfExists = strings.ToLower(dropExists) == "true"
	}
	if maxConn := os.Getenv("PGFORK_MAX_CONNECTIONS"); maxConn != "" {
		if m, err := strconv.Atoi(maxConn); err == nil {
			c.MaxConnections = m
		}
	}
	if chunkSize := os.Getenv("PGFORK_CHUNK_SIZE"); chunkSize != "" {
		if cs, err := strconv.Atoi(chunkSize); err == nil {
			c.ChunkSize = cs
		}
	}
	if timeout := os.Getenv("PGFORK_TIMEOUT"); timeout != "" {
		if t, err := time.ParseDuration(timeout); err == nil {
			c.Timeout = t
		}
	}

	// CI/CD specific configuration
	if outputFormat := os.Getenv("PGFORK_OUTPUT_FORMAT"); outputFormat != "" {
		c.OutputFormat = outputFormat
	}
	if quiet := os.Getenv("PGFORK_QUIET"); quiet != "" {
		c.Quiet = strings.ToLower(quiet) == "true"
	}
	if dryRun := os.Getenv("PGFORK_DRY_RUN"); dryRun != "" {
		c.DryRun = strings.ToLower(dryRun) == "true"
	}

	// Load template variables from environment
	if c.TemplateVars == nil {
		c.TemplateVars = make(map[string]string)
	}
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "PGFORK_VAR_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				varName := strings.TrimPrefix(parts[0], "PGFORK_VAR_")
				c.TemplateVars[varName] = parts[1]
			}
		}
	}
}

// ProcessTemplates processes template variables in configuration strings
func (c *ForkConfig) ProcessTemplates() error {
	// Process target database name
	if strings.Contains(c.TargetDatabase, "{{") {
		processed, err := c.processTemplate(c.TargetDatabase)
		if err != nil {
			return fmt.Errorf("failed to process target database template: %w", err)
		}
		c.TargetDatabase = processed
	}

	// Process source database name if it contains templates
	if strings.Contains(c.Source.Database, "{{") {
		processed, err := c.processTemplate(c.Source.Database)
		if err != nil {
			return fmt.Errorf("failed to process source database template: %w", err)
		}
		c.Source.Database = processed
	}

	return nil
}

// processTemplate processes a single template string
func (c *ForkConfig) processTemplate(templateStr string) (string, error) {
	tmpl, err := template.New("config").Parse(templateStr)
	if err != nil {
		return "", err
	}

	// Merge template variables with common CI/CD variables
	vars := make(map[string]string)
	for k, v := range c.TemplateVars {
		vars[k] = v
	}

	// Add common CI/CD environment variables
	if prNumber := os.Getenv("GITHUB_PR_NUMBER"); prNumber != "" {
		vars["PR_NUMBER"] = prNumber
	}
	if prNumber := os.Getenv("CI_MERGE_REQUEST_IID"); prNumber != "" {
		vars["PR_NUMBER"] = prNumber
	}
	if branch := os.Getenv("GITHUB_HEAD_REF"); branch != "" {
		vars["BRANCH"] = sanitizeBranchName(branch)
	}
	if branch := os.Getenv("CI_COMMIT_REF_NAME"); branch != "" {
		vars["BRANCH"] = sanitizeBranchName(branch)
	}
	if commit := os.Getenv("GITHUB_SHA"); commit != "" && len(commit) >= 8 {
		vars["COMMIT_SHORT"] = commit[:8]
	}
	if commit := os.Getenv("CI_COMMIT_SHA"); commit != "" && len(commit) >= 8 {
		vars["COMMIT_SHORT"] = commit[:8]
	}

	var result strings.Builder
	err = tmpl.Execute(&result, vars)
	if err != nil {
		return "", err
	}

	return result.String(), nil
}

// sanitizeBranchName converts branch names to valid database identifiers
func sanitizeBranchName(branch string) string {
	// Replace invalid characters with underscores
	result := strings.ReplaceAll(branch, "/", "_")
	result = strings.ReplaceAll(result, "-", "_")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.ToLower(result)

	// Ensure it starts with a letter or underscore
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "br_" + result
	}

	// Limit length to 63 characters (PostgreSQL identifier limit)
	if len(result) > 63 {
		result = result[:63]
	}

	return result
}

// Validate checks if the configuration is valid using struct tags and custom logic
func (c *ForkConfig) Validate() error {
	// First, run struct tag validation
	if err := validate.Struct(c); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			return formatValidationErrors(validationErrors)
		}
		return fmt.Errorf("validation failed: %w", err)
	}

	// Custom business logic validation
	if err := c.validateBusinessLogic(); err != nil {
		return err
	}

	return nil
}

// validateBusinessLogic performs custom validation that can't be expressed with struct tags
func (c *ForkConfig) validateBusinessLogic() error {
	// Validate conflicting options
	if c.SchemaOnly && c.DataOnly {
		return fmt.Errorf("cannot specify both schema-only and data-only options")
	}

	// Validate same database on same server
	if c.Source.Database == c.TargetDatabase && c.IsSameServer() {
		return fmt.Errorf("source and target databases cannot be the same on the same server")
	}

	// Validate table filtering conflicts
	if len(c.IncludeTables) > 0 && len(c.ExcludeTables) > 0 {
		// Check for overlapping tables
		includeMap := make(map[string]bool)
		for _, table := range c.IncludeTables {
			includeMap[table] = true
		}

		for _, table := range c.ExcludeTables {
			if includeMap[table] {
				return fmt.Errorf("table '%s' cannot be both included and excluded", table)
			}
		}
	}

	// Validate URI vs individual parameters
	if err := c.Source.validateURIConsistency(); err != nil {
		return fmt.Errorf("source configuration: %w", err)
	}

	if err := c.Destination.validateURIConsistency(); err != nil {
		return fmt.Errorf("destination configuration: %w", err)
	}

	return nil
}

// validateURIConsistency ensures URI and individual parameters are not conflicting
func (d *DatabaseConfig) validateURIConsistency() error {
	if d.URI != "" {
		// If URI is provided, individual parameters should be empty or consistent
		parsed, err := parsePostgreSQLURI(d.URI)
		if err != nil {
			return fmt.Errorf("invalid URI: %w", err)
		}

		// Check for conflicts
		if d.Host != "" && d.Host != parsed.Host {
			return fmt.Errorf("URI host conflicts with individual host parameter")
		}
		if d.Port != 0 && d.Port != parsed.Port {
			return fmt.Errorf("URI port conflicts with individual port parameter")
		}
		if d.Username != "" && d.Username != parsed.Username {
			return fmt.Errorf("URI username conflicts with individual username parameter")
		}
		if d.Database != "" && d.Database != parsed.Database {
			return fmt.Errorf("URI database conflicts with individual database parameter")
		}
	}

	return nil
}

// formatValidationErrors converts validator errors into user-friendly messages
func formatValidationErrors(errs validator.ValidationErrors) error {
	var messages []string

	for _, err := range errs {
		var message string

		switch err.Tag() {
		case "required":
			message = fmt.Sprintf("%s is required", err.Field())
		case "required_without":
			message = fmt.Sprintf("%s is required when %s is not provided", err.Field(), err.Param())
		case "min":
			message = fmt.Sprintf("%s must be at least %s", err.Field(), err.Param())
		case "max":
			message = fmt.Sprintf("%s must be at most %s", err.Field(), err.Param())
		case "oneof":
			message = fmt.Sprintf("%s must be one of: %s", err.Field(), err.Param())
		case "hostname_rfc1123":
			message = fmt.Sprintf("%s must be a valid hostname", err.Field())
		case "ip":
			message = fmt.Sprintf("%s must be a valid IP address", err.Field())
		case "uri":
			message = fmt.Sprintf("%s must be a valid URI", err.Field())
		default:
			message = fmt.Sprintf("%s validation failed: %s", err.Field(), err.Tag())
		}

		messages = append(messages, message)
	}

	return fmt.Errorf("validation failed: %s", strings.Join(messages, "; "))
}
