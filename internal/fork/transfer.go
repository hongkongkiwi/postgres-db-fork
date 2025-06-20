package fork

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"
	"github.com/hongkongkiwi/postgres-db-fork/internal/logging"

	"github.com/schollz/progressbar/v3"
)

// DataTransferManager handles cross-server data transfer with optimizations
type DataTransferManager struct {
	source      *db.Connection
	dest        *db.Connection
	sourceCfg   *config.DatabaseConfig
	destCfg     *config.DatabaseConfig
	config      *config.ForkConfig
	progressBar *progressbar.ProgressBar
	metrics     MetricsUpdater
	logger      *logging.Logger
}

// MetricsUpdater interface for updating metrics
type MetricsUpdater interface {
	updateMetrics(bytesTransferred, rowsTransferred int64)
	incrementTableCount()
}

// NewDataTransferManager creates a new data transfer manager
func NewDataTransferManager(source *db.Connection, dest *db.Connection, sourceCfg, destCfg *config.DatabaseConfig, cfg *config.ForkConfig, logger *logging.Logger) *DataTransferManager {
	return &DataTransferManager{
		source:    source,
		dest:      dest,
		sourceCfg: sourceCfg,
		destCfg:   destCfg,
		config:    cfg,
		logger:    logger,
	}
}

// SetMetricsUpdater sets the metrics updater
func (dtm *DataTransferManager) SetMetricsUpdater(updater MetricsUpdater) {
	dtm.metrics = updater
}

// Transfer executes the complete data transfer with optimizations
func (dtm *DataTransferManager) Transfer(ctx context.Context) error {
	dtm.logger.Info("Starting optimized cross-server data transfer...")

	// Get list of tables for progress bar setup
	tables, err := dtm.source.GetTableList("public")
	if err != nil {
		return fmt.Errorf("failed to get table list: %w", err)
	}

	// Filter tables based on include/exclude lists
	tables = dtm.filterTables(tables)

	// Create progress bar if not in quiet mode
	if !dtm.config.Quiet && len(tables) > 0 {
		dtm.progressBar = progressbar.NewOptions(len(tables),
			progressbar.OptionSetDescription("Transferring tables..."),
			progressbar.OptionSetWidth(50),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "█",
				SaucerHead:    "█",
				SaucerPadding: "░",
				BarStart:      "│",
				BarEnd:        "│",
			}),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetPredictTime(true),
		)
	}

	// Optimize destination database for bulk loading
	if err := dtm.optimizeDestination(); err != nil {
		return fmt.Errorf("failed to optimize destination: %w", err)
	}

	// First, transfer schema if not data-only
	if !dtm.config.DataOnly {
		if err := dtm.transferSchema(ctx); err != nil {
			return fmt.Errorf("failed to transfer schema: %w", err)
		}
	}

	// Then, transfer data if not schema-only
	if !dtm.config.SchemaOnly {
		if err := dtm.transferDataOptimized(ctx); err != nil {
			return fmt.Errorf("failed to transfer data: %w", err)
		}
	}

	// Restore normal database settings
	if err := dtm.restoreDestination(); err != nil {
		dtm.logger.Warnf("Failed to restore destination settings: %v", err)
	}

	dtm.logger.Info("Optimized cross-server data transfer completed successfully")
	return nil
}

// optimizeDestination configures the destination database for maximum write performance
func (dtm *DataTransferManager) optimizeDestination() error {
	dtm.logger.Info("Optimizing destination database for bulk loading...")

	optimizations := []string{
		"SET synchronous_commit = OFF",
		"SET wal_buffers = '16MB'",
		"SET checkpoint_segments = 32", // For older PostgreSQL versions
		"SET checkpoint_completion_target = 0.9",
		"SET wal_compression = ON",
		"SET max_wal_size = '1GB'",
		"SET shared_buffers = '256MB'", // Will be ignored if already set higher
	}

	for _, sql := range optimizations {
		if _, err := dtm.dest.DB.Exec(sql); err != nil {
			dtm.logger.Debugf("Optimization setting failed (may not be supported): %s - %v", sql, err)
		}
	}

	return nil
}

// restoreDestination restores normal database settings
func (dtm *DataTransferManager) restoreDestination() error {
	dtm.logger.Debug("Restoring destination database settings...")

	restorations := []string{
		"SET synchronous_commit = ON",
		"CHECKPOINT", // Force a checkpoint after bulk loading
	}

	for _, sql := range restorations {
		if _, err := dtm.dest.DB.Exec(sql); err != nil {
			dtm.logger.Debugf("Restoration setting failed: %s - %v", sql, err)
		}
	}

	return nil
}

// transferSchema transfers the database schema using a pg_dump pipeline for reliability
func (dtm *DataTransferManager) transferSchema(ctx context.Context) error {
	dtm.logger.Info("Transferring database schema using pg_dump | pg_restore...")

	reader, writer := io.Pipe()
	defer dtm.closePipe(reader, "schema reader")
	defer dtm.closePipe(writer, "schema writer")

	// Configure pg_dump for schema only
	dumpArgs := []string{
		"--schema-only",
		"--format=custom",
		"--no-comments",
		"--no-security-labels",
		"--no-tablespaces",
		"--no-owner",
		"--no-privileges",
		"-d", dtm.sourceCfg.ConnectionString(),
	}

	// Add table filtering if specified
	if len(dtm.config.IncludeTables) > 0 {
		// If include list is specified, only include those tables (ignore exclude list)
		for _, table := range dtm.config.IncludeTables {
			dumpArgs = append(dumpArgs, "--table="+table)
		}
	} else if len(dtm.config.ExcludeTables) > 0 {
		// Only apply exclude list if no include list is specified
		for _, table := range dtm.config.ExcludeTables {
			dumpArgs = append(dumpArgs, "--exclude-table="+table)
		}
	}

	dumpCmd := exec.CommandContext(ctx, "pg_dump", dumpArgs...)
	dumpCmd.Stdout = writer
	dumpCmd.Stderr = os.Stderr // Forward errors for visibility

	// Configure pg_restore
	restoreCmd := exec.CommandContext(ctx, "pg_restore",
		"-d", dtm.destCfg.ConnectionString(),
	)
	restoreCmd.Stdin = reader
	restoreCmd.Stdout = os.Stdout
	restoreCmd.Stderr = os.Stderr

	// Set environment variables for authentication
	dumpCmd.Env = append(os.Environ(), "PGPASSWORD="+dtm.sourceCfg.Password)
	restoreCmd.Env = append(os.Environ(), "PGPASSWORD="+dtm.destCfg.Password)

	// Start both commands
	if err := dumpCmd.Start(); err != nil {
		return fmt.Errorf("failed to start pg_dump (schema): %w", err)
	}
	if err := restoreCmd.Start(); err != nil {
		return fmt.Errorf("failed to start pg_restore (schema): %w", err)
	}

	// Wait for pg_dump to finish and close the writer
	dumpErrChan := make(chan error, 1)
	go func() {
		defer dtm.closePipe(writer, "schema writer")
		dumpErrChan <- dumpCmd.Wait()
	}()

	// Wait for pg_restore to finish
	restoreErr := restoreCmd.Wait()
	dumpErr := <-dumpErrChan

	if dumpErr != nil {
		return fmt.Errorf("pg_dump (schema) failed: %w", dumpErr)
	}
	if restoreErr != nil {
		// Check if this is just a warning about ignored errors (common with version mismatches)
		if exitErr, ok := restoreErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			dtm.logger.Warnf("pg_restore completed with warnings (exit code 1), continuing...")
		} else {
			return fmt.Errorf("pg_restore (schema) failed: %w", restoreErr)
		}
	}

	dtm.logger.Info("Schema transfer completed successfully")
	return nil
}

// transferDataOptimized transfers data using pg_dump and pg_restore for maximum performance
func (dtm *DataTransferManager) transferDataOptimized(ctx context.Context) error {
	dtm.logger.Info("Transferring database data using optimized pg_dump | pg_restore pipeline...")

	// Create a pipe to connect pg_dump's output to pg_restore's input
	reader, writer := io.Pipe()
	defer dtm.closePipe(reader, "data reader")
	defer dtm.closePipe(writer, "data writer")

	// Configure pg_dump command
	dumpArgs := []string{
		"--data-only",
		"--format=custom", // Use custom format for pg_restore
		"--no-comments",
		"--no-security-labels",
		"--no-tablespaces",
		"--no-owner",
		"--no-privileges",
		"-d", dtm.sourceCfg.ConnectionString(),
	}

	// Add table filtering if specified
	if len(dtm.config.IncludeTables) > 0 {
		// If include list is specified, only include those tables (ignore exclude list)
		for _, table := range dtm.config.IncludeTables {
			dumpArgs = append(dumpArgs, "--table="+table)
		}
	} else if len(dtm.config.ExcludeTables) > 0 {
		// Only apply exclude list if no include list is specified
		for _, table := range dtm.config.ExcludeTables {
			dumpArgs = append(dumpArgs, "--exclude-table="+table)
		}
	}

	dumpCmd := exec.CommandContext(ctx, "pg_dump", dumpArgs...)
	dumpCmd.Stdout = writer
	dumpCmd.Stderr = os.Stderr // Forward errors to stderr for visibility

	// Configure pg_restore command
	restoreCmd := exec.CommandContext(ctx, "pg_restore",
		"--data-only",
		"-d", dtm.destCfg.ConnectionString(),
	)
	restoreCmd.Stdin = reader
	restoreCmd.Stdout = os.Stdout
	restoreCmd.Stderr = os.Stderr

	// Set environment variables for authentication
	dumpCmd.Env = append(os.Environ(), "PGPASSWORD="+dtm.sourceCfg.Password)
	restoreCmd.Env = append(os.Environ(), "PGPASSWORD="+dtm.destCfg.Password)

	// Start both commands
	if err := dumpCmd.Start(); err != nil {
		return fmt.Errorf("failed to start pg_dump: %w", err)
	}
	if err := restoreCmd.Start(); err != nil {
		return fmt.Errorf("failed to start pg_restore: %w", err)
	}

	// Wait for pg_dump to finish and close the writer
	dumpErrChan := make(chan error, 1)
	go func() {
		defer dtm.closePipe(writer, "data writer")
		dumpErrChan <- dumpCmd.Wait()
	}()

	// Wait for pg_restore to finish
	restoreErr := restoreCmd.Wait()
	dumpErr := <-dumpErrChan

	if dumpErr != nil {
		return fmt.Errorf("pg_dump failed: %w", dumpErr)
	}
	if restoreErr != nil {
		// Check if this is just a warning about ignored errors (common with version mismatches)
		if exitErr, ok := restoreErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			dtm.logger.Warnf("pg_restore completed with warnings (exit code 1), continuing...")
		} else {
			return fmt.Errorf("pg_restore failed: %w", restoreErr)
		}
	}

	dtm.logger.Info("Optimized data transfer completed successfully")
	return nil
}

// closePipe is a helper to close an io.Closer and log any error
func (dtm *DataTransferManager) closePipe(closer io.Closer, name string) {
	if err := closer.Close(); err != nil {
		dtm.logger.Warnf("Failed to close %s: %v", name, err)
	}
}

// filterTables filters tables based on include/exclude configuration
func (dtm *DataTransferManager) filterTables(tables []string) []string {
	if len(dtm.config.IncludeTables) > 0 {
		// If include list is specified, only include those tables (ignore exclude list)
		var filtered []string
		includeMap := make(map[string]bool)
		for _, table := range dtm.config.IncludeTables {
			includeMap[table] = true
		}
		for _, table := range tables {
			if includeMap[table] {
				filtered = append(filtered, table)
			}
		}
		return filtered
	}

	if len(dtm.config.ExcludeTables) > 0 {
		// Only apply exclude list if no include list is specified
		excludeMap := make(map[string]bool)
		for _, table := range dtm.config.ExcludeTables {
			excludeMap[table] = true
		}
		var filtered []string
		for _, table := range tables {
			if !excludeMap[table] {
				filtered = append(filtered, table)
			}
		}
		return filtered
	}

	return tables
}
