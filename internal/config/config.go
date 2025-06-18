package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// DatabaseConfig represents a PostgreSQL database connection configuration
type DatabaseConfig struct {
	Host     string `mapstructure:"host" yaml:"host"`
	Port     int    `mapstructure:"port" yaml:"port"`
	Username string `mapstructure:"username" yaml:"username"`
	Password string `mapstructure:"password" yaml:"password"`
	Database string `mapstructure:"database" yaml:"database"`
	SSLMode  string `mapstructure:"sslmode" yaml:"sslmode"`
}

// ForkConfig represents the configuration for a database fork operation
type ForkConfig struct {
	Source         DatabaseConfig `mapstructure:"source" yaml:"source"`
	Destination    DatabaseConfig `mapstructure:"destination" yaml:"destination"`
	TargetDatabase string         `mapstructure:"target_database" yaml:"target_database"`
	DropIfExists   bool           `mapstructure:"drop_if_exists" yaml:"drop_if_exists"`
	MaxConnections int            `mapstructure:"max_connections" yaml:"max_connections"`
	ChunkSize      int            `mapstructure:"chunk_size" yaml:"chunk_size"`
	Timeout        time.Duration  `mapstructure:"timeout" yaml:"timeout"`
	ExcludeTables  []string       `mapstructure:"exclude_tables" yaml:"exclude_tables"`
	IncludeTables  []string       `mapstructure:"include_tables" yaml:"include_tables"`
	SchemaOnly     bool           `mapstructure:"schema_only" yaml:"schema_only"`
	DataOnly       bool           `mapstructure:"data_only" yaml:"data_only"`

	// CI/CD Integration features
	OutputFormat string            `mapstructure:"output_format" yaml:"output_format"`
	Quiet        bool              `mapstructure:"quiet" yaml:"quiet"`
	DryRun       bool              `mapstructure:"dry_run" yaml:"dry_run"`
	TemplateVars map[string]string `mapstructure:"template_vars" yaml:"template_vars"`
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

// ConnectionString builds a PostgreSQL connection string from the config
func (c *DatabaseConfig) ConnectionString() string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "prefer"
	}

	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
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
	// Source configuration
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

	// Destination configuration
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

// Validate checks if the configuration is valid
func (c *ForkConfig) Validate() error {
	if c.Source.Host == "" {
		return fmt.Errorf("source host is required")
	}
	if c.Source.Database == "" {
		return fmt.Errorf("source database is required")
	}
	if c.Destination.Host == "" {
		return fmt.Errorf("destination host is required")
	}
	if c.TargetDatabase == "" {
		return fmt.Errorf("target database name is required")
	}
	if c.Source.Database == c.TargetDatabase && c.IsSameServer() {
		return fmt.Errorf("source and target databases cannot be the same")
	}
	if c.MaxConnections <= 0 {
		c.MaxConnections = 4
	}
	if c.ChunkSize <= 0 {
		c.ChunkSize = 1000
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Minute
	}

	// Validate output format
	if c.OutputFormat != "" && c.OutputFormat != "json" && c.OutputFormat != "text" {
		return fmt.Errorf("output format must be 'json' or 'text'")
	}

	return nil
}
