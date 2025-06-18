package fork

import (
	"context"
	"fmt"
	"time"

	"postgres-db-fork/internal/config"
	"postgres-db-fork/internal/db"

	"github.com/sirupsen/logrus"
)

// Forker handles database forking operations
type Forker struct {
	config *config.ForkConfig
}

// NewForker creates a new database forker
func NewForker(cfg *config.ForkConfig) *Forker {
	return &Forker{
		config: cfg,
	}
}

// Fork executes the database fork operation
func (f *Forker) Fork(ctx context.Context) error {
	if err := f.config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	logrus.Infof("Starting database fork operation...")
	logrus.Infof("Source: %s:%d/%s", f.config.Source.Host, f.config.Source.Port, f.config.Source.Database)
	logrus.Infof("Target: %s:%d/%s", f.config.Destination.Host, f.config.Destination.Port, f.config.TargetDatabase)

	start := time.Now()

	var err error
	if f.config.IsSameServer() {
		logrus.Info("Detected same-server fork, using efficient template-based cloning")
		err = f.forkSameServer(ctx)
	} else {
		logrus.Info("Detected cross-server fork, using dump and restore")
		err = f.forkCrossServer(ctx)
	}

	if err != nil {
		return err
	}

	duration := time.Since(start)
	logrus.Infof("Database fork completed successfully in %v", duration)
	return nil
}

// forkSameServer handles same-server forking using PostgreSQL templates
func (f *Forker) forkSameServer(ctx context.Context) error {
	// Connect to the destination server (using postgres database for admin operations)
	adminConfig := f.config.Destination
	adminConfig.Database = "postgres"

	conn, err := db.NewConnection(&adminConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to destination server: %w", err)
	}
	defer conn.Close()

	// Check if source database exists
	exists, err := conn.DatabaseExists(f.config.Source.Database)
	if err != nil {
		return fmt.Errorf("failed to check source database: %w", err)
	}
	if !exists {
		return fmt.Errorf("source database '%s' does not exist", f.config.Source.Database)
	}

	// Check if target database exists
	targetExists, err := conn.DatabaseExists(f.config.TargetDatabase)
	if err != nil {
		return fmt.Errorf("failed to check target database: %w", err)
	}

	if targetExists {
		if f.config.DropIfExists {
			if err := conn.DropDatabase(f.config.TargetDatabase); err != nil {
				return fmt.Errorf("failed to drop existing target database: %w", err)
			}
		} else {
			return fmt.Errorf("target database '%s' already exists (use --drop-if-exists to overwrite)", f.config.TargetDatabase)
		}
	}

	// Get source database size for progress reporting
	sourceSize, err := conn.GetDatabaseSize(f.config.Source.Database)
	if err != nil {
		logrus.Warnf("Could not get source database size: %v", err)
	} else {
		logrus.Infof("Source database size: %s", formatBytes(sourceSize))
	}

	// Create the target database using the source as template
	if err := conn.CreateDatabase(f.config.TargetDatabase, f.config.Source.Database); err != nil {
		return fmt.Errorf("failed to create target database: %w", err)
	}

	// Verify the fork was successful
	targetSize, err := conn.GetDatabaseSize(f.config.TargetDatabase)
	if err != nil {
		logrus.Warnf("Could not verify target database size: %v", err)
	} else {
		logrus.Infof("Target database size: %s", formatBytes(targetSize))
	}

	return nil
}

// forkCrossServer handles cross-server forking using dump and restore
func (f *Forker) forkCrossServer(ctx context.Context) error {
	// This is a more complex operation that would involve:
	// 1. Creating a custom dump and restore mechanism
	// 2. Or using pg_dump and pg_restore utilities
	// 3. Or streaming data table by table

	// Connect to source database
	sourceConn, err := db.NewConnection(&f.config.Source)
	if err != nil {
		return fmt.Errorf("failed to connect to source database: %w", err)
	}
	defer sourceConn.Close()

	// Connect to destination server for admin operations
	adminConfig := f.config.Destination
	adminConfig.Database = "postgres"

	destAdminConn, err := db.NewConnection(&adminConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to destination server: %w", err)
	}
	defer destAdminConn.Close()

	// Check if target database exists
	targetExists, err := destAdminConn.DatabaseExists(f.config.TargetDatabase)
	if err != nil {
		return fmt.Errorf("failed to check target database: %w", err)
	}

	if targetExists {
		if f.config.DropIfExists {
			if err := destAdminConn.DropDatabase(f.config.TargetDatabase); err != nil {
				return fmt.Errorf("failed to drop existing target database: %w", err)
			}
		} else {
			return fmt.Errorf("target database '%s' already exists (use --drop-if-exists to overwrite)", f.config.TargetDatabase)
		}
	}

	// Create empty target database
	if err := destAdminConn.CreateDatabase(f.config.TargetDatabase, ""); err != nil {
		return fmt.Errorf("failed to create target database: %w", err)
	}

	// Connect to the target database
	targetConfig := f.config.Destination
	targetConfig.Database = f.config.TargetDatabase

	destConn, err := db.NewConnection(&targetConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to target database: %w", err)
	}
	defer destConn.Close()

	// Get source database size for progress reporting
	sourceSize, err := sourceConn.GetDatabaseSize(f.config.Source.Database)
	if err != nil {
		logrus.Warnf("Could not get source database size: %v", err)
	} else {
		logrus.Infof("Source database size: %s", formatBytes(sourceSize))
	}

	// Create a data transfer manager
	transferManager := NewDataTransferManager(sourceConn, destConn, f.config)

	// Execute the data transfer
	if err := transferManager.Transfer(ctx); err != nil {
		return fmt.Errorf("failed to transfer data: %w", err)
	}

	return nil
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
