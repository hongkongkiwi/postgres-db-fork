package fork

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"postgres-db-fork/internal/config"
	"postgres-db-fork/internal/db"

	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

// DataTransferManager handles cross-server data transfer with optimizations
type DataTransferManager struct {
	source *db.Connection
	dest   *db.Connection
	config *config.ForkConfig
}

// NewDataTransferManager creates a new data transfer manager
func NewDataTransferManager(source, dest *db.Connection, cfg *config.ForkConfig) *DataTransferManager {
	return &DataTransferManager{
		source: source,
		dest:   dest,
		config: cfg,
	}
}

// Transfer executes the complete data transfer with optimizations
func (dtm *DataTransferManager) Transfer(ctx context.Context) error {
	logrus.Info("Starting optimized cross-server data transfer...")

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
		logrus.Warnf("Failed to restore destination settings: %v", err)
	}

	logrus.Info("Optimized cross-server data transfer completed successfully")
	return nil
}

// optimizeDestination configures the destination database for maximum write performance
func (dtm *DataTransferManager) optimizeDestination() error {
	logrus.Info("Optimizing destination database for bulk loading...")

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
			logrus.Debugf("Optimization setting failed (may not be supported): %s - %v", sql, err)
		}
	}

	return nil
}

// restoreDestination restores normal database settings
func (dtm *DataTransferManager) restoreDestination() error {
	logrus.Debug("Restoring destination database settings...")

	restorations := []string{
		"SET synchronous_commit = ON",
		"CHECKPOINT", // Force a checkpoint after bulk loading
	}

	for _, sql := range restorations {
		if _, err := dtm.dest.DB.Exec(sql); err != nil {
			logrus.Debugf("Restoration setting failed: %s - %v", sql, err)
		}
	}

	return nil
}

// transferSchema transfers the database schema with better DDL extraction
func (dtm *DataTransferManager) transferSchema(ctx context.Context) error {
	logrus.Info("Transferring database schema...")

	// Get comprehensive schema information
	schemaSQL, err := dtm.getCompleteSchemaSQL()
	if err != nil {
		return fmt.Errorf("failed to get schema SQL: %w", err)
	}

	// Execute schema creation on destination
	if err := dtm.executeSchemaSQL(schemaSQL); err != nil {
		return fmt.Errorf("failed to execute schema SQL: %w", err)
	}

	logrus.Info("Schema transfer completed")
	return nil
}

// transferDataOptimized transfers data using COPY for maximum performance
func (dtm *DataTransferManager) transferDataOptimized(ctx context.Context) error {
	logrus.Info("Transferring database data using optimized COPY operations...")

	// Get list of tables to transfer
	tables, err := dtm.source.GetTableList("public")
	if err != nil {
		return fmt.Errorf("failed to get table list: %w", err)
	}

	// Filter tables based on include/exclude lists
	tables = dtm.filterTables(tables)

	if len(tables) == 0 {
		logrus.Info("No tables to transfer")
		return nil
	}

	logrus.Infof("Transferring %d tables using parallel COPY operations", len(tables))

	// Transfer tables with limited concurrency using COPY
	sem := make(chan struct{}, dtm.config.MaxConnections)
	var wg sync.WaitGroup
	errChan := make(chan error, len(tables))

	for _, table := range tables {
		wg.Add(1)
		go func(tableName string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire semaphore
			defer func() { <-sem }() // release semaphore

			if err := dtm.transferTableOptimized(ctx, tableName); err != nil {
				errChan <- fmt.Errorf("failed to transfer table %s: %w", tableName, err)
				return
			}
			logrus.Infof("Successfully transferred table: %s", tableName)
		}(table)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		return err
	}

	logrus.Info("Optimized data transfer completed")
	return nil
}

// transferTableOptimized transfers a single table using COPY for maximum speed
func (dtm *DataTransferManager) transferTableOptimized(ctx context.Context, tableName string) error {
	logrus.Debugf("Transferring table using COPY: %s", tableName)

	// Get row count for progress reporting
	var rowCount int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", pq.QuoteIdentifier(tableName))
	if err := dtm.source.DB.QueryRow(countQuery).Scan(&rowCount); err != nil {
		logrus.Warnf("Could not get row count for table %s: %v", tableName, err)
	} else {
		logrus.Debugf("Table %s has %d rows", tableName, rowCount)
	}

	// Get column information for proper ordering
	columns, err := dtm.getTableColumns(tableName)
	if err != nil {
		return fmt.Errorf("failed to get columns for table %s: %w", tableName, err)
	}

	if len(columns) == 0 {
		logrus.Debugf("Table %s has no columns, skipping", tableName)
		return nil
	}

	// Use COPY for maximum performance
	return dtm.copyTableData(ctx, tableName, columns, rowCount)
}

// copyTableData uses PostgreSQL COPY for maximum transfer speed
func (dtm *DataTransferManager) copyTableData(ctx context.Context, tableName string, columns []string, estimatedRows int64) error {
	quotedTable := pq.QuoteIdentifier(tableName)
	quotedColumns := make([]string, len(columns))
	for i, col := range columns {
		quotedColumns[i] = pq.QuoteIdentifier(col)
	}

	// Start COPY FROM on destination
	copySQL := fmt.Sprintf("COPY %s (%s) FROM STDIN WITH (FORMAT CSV, DELIMITER ',', QUOTE '\"', ESCAPE '\"', NULL '\\N')",
		quotedTable, strings.Join(quotedColumns, ", "))

	// Begin transaction for atomic transfer
	destTx, err := dtm.dest.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin destination transaction: %w", err)
	}
	defer destTx.Rollback()

	// Prepare COPY FROM statement
	stmt, err := destTx.PrepareContext(ctx, copySQL)
	if err != nil {
		return fmt.Errorf("failed to prepare COPY statement: %w", err)
	}
	defer stmt.Close()

	// Create a pipe for streaming data
	pipeReader, pipeWriter := io.Pipe()
	defer pipeReader.Close()

	// Start the COPY operation in a goroutine
	copyErrChan := make(chan error, 1)
	go func() {
		defer pipeWriter.Close()
		_, err := stmt.ExecContext(ctx, pipeReader)
		copyErrChan <- err
	}()

	// Stream data from source using COPY TO
	sourceCopySQL := fmt.Sprintf("COPY %s (%s) TO STDOUT WITH (FORMAT CSV, DELIMITER ',', QUOTE '\"', ESCAPE '\"', NULL '\\N')",
		quotedTable, strings.Join(quotedColumns, ", "))

	rows, err := dtm.source.DB.QueryContext(ctx, sourceCopySQL)
	if err != nil {
		pipeWriter.Close()
		return fmt.Errorf("failed to start source COPY: %w", err)
	}
	defer rows.Close()

	// Stream data through the pipe
	var transferredRows int64
	for rows.Next() {
		var csvLine string
		if err := rows.Scan(&csvLine); err != nil {
			pipeWriter.CloseWithError(err)
			return fmt.Errorf("failed to scan row: %w", err)
		}

		if _, err := pipeWriter.Write([]byte(csvLine + "\n")); err != nil {
			return fmt.Errorf("failed to write to pipe: %w", err)
		}

		transferredRows++
		if transferredRows%10000 == 0 {
			logrus.Debugf("Transferred %d/%d rows from table %s", transferredRows, estimatedRows, tableName)
		}
	}

	if err := rows.Err(); err != nil {
		pipeWriter.CloseWithError(err)
		return fmt.Errorf("error during row iteration: %w", err)
	}

	// Close the writer to signal end of data
	pipeWriter.Close()

	// Wait for COPY operation to complete
	if err := <-copyErrChan; err != nil {
		return fmt.Errorf("COPY operation failed: %w", err)
	}

	// Commit the transaction
	if err := destTx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	logrus.Debugf("Successfully transferred %d rows from table %s using COPY", transferredRows, tableName)
	return nil
}

// getTableColumns returns the column names for a table in the correct order
func (dtm *DataTransferManager) getTableColumns(tableName string) ([]string, error) {
	query := `
		SELECT column_name 
		FROM information_schema.columns 
		WHERE table_name = $1 AND table_schema = 'public'
		ORDER BY ordinal_position`

	rows, err := dtm.source.DB.Query(query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}

	return columns, rows.Err()
}

// getCompleteSchemaSQL generates comprehensive SQL to recreate the database schema
func (dtm *DataTransferManager) getCompleteSchemaSQL() ([]string, error) {
	var schemas []string

	// Get table definitions with proper column types and constraints
	tableSQL, err := dtm.getTableDefinitions()
	if err != nil {
		return nil, fmt.Errorf("failed to get table definitions: %w", err)
	}
	schemas = append(schemas, tableSQL...)

	// Get indexes (excluding primary keys which are created with tables)
	indexSQL, err := dtm.getIndexDefinitions()
	if err != nil {
		logrus.Warnf("Failed to get index definitions: %v", err)
	} else {
		schemas = append(schemas, indexSQL...)
	}

	// Get foreign key constraints
	fkSQL, err := dtm.getForeignKeyDefinitions()
	if err != nil {
		logrus.Warnf("Failed to get foreign key definitions: %v", err)
	} else {
		schemas = append(schemas, fkSQL...)
	}

	return schemas, nil
}

// getTableDefinitions gets complete table definitions
func (dtm *DataTransferManager) getTableDefinitions() ([]string, error) {
	query := `
		SELECT 
			'CREATE TABLE ' || quote_ident(schemaname) || '.' || quote_ident(tablename) || ' (' ||
			array_to_string(
				array_agg(
					quote_ident(column_name) || ' ' || 
					CASE 
						WHEN data_type = 'character varying' THEN 'varchar(' || character_maximum_length || ')'
						WHEN data_type = 'character' THEN 'char(' || character_maximum_length || ')'
						WHEN data_type = 'numeric' THEN 'numeric(' || numeric_precision || ',' || numeric_scale || ')'
						ELSE data_type
					END ||
					CASE WHEN is_nullable = 'NO' THEN ' NOT NULL' ELSE '' END ||
					CASE WHEN column_default IS NOT NULL THEN ' DEFAULT ' || column_default ELSE '' END
					ORDER BY ordinal_position
				), 
				', '
			) ||
			');' as create_table_sql
		FROM information_schema.columns
		JOIN pg_tables ON pg_tables.tablename = information_schema.columns.table_name
		WHERE schemaname = 'public'
		GROUP BY schemaname, tablename`

	rows, err := dtm.source.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var tableSQL string
		if err := rows.Scan(&tableSQL); err != nil {
			return nil, err
		}
		schemas = append(schemas, tableSQL)
	}

	return schemas, rows.Err()
}

// getIndexDefinitions gets index creation statements
func (dtm *DataTransferManager) getIndexDefinitions() ([]string, error) {
	query := `
		SELECT indexdef 
		FROM pg_indexes 
		WHERE schemaname = 'public' 
		  AND indexname NOT LIKE '%_pkey'`

	rows, err := dtm.source.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []string
	for rows.Next() {
		var indexSQL string
		if err := rows.Scan(&indexSQL); err != nil {
			return nil, err
		}
		indexes = append(indexes, indexSQL+";")
	}

	return indexes, rows.Err()
}

// getForeignKeyDefinitions gets foreign key constraint definitions
func (dtm *DataTransferManager) getForeignKeyDefinitions() ([]string, error) {
	query := `
		SELECT 'ALTER TABLE ' || quote_ident(tc.table_schema) || '.' || quote_ident(tc.table_name) ||
			   ' ADD CONSTRAINT ' || quote_ident(tc.constraint_name) ||
			   ' FOREIGN KEY (' || string_agg(quote_ident(kcu.column_name), ', ') || ')' ||
			   ' REFERENCES ' || quote_ident(ccu.table_schema) || '.' || quote_ident(ccu.table_name) ||
			   ' (' || string_agg(quote_ident(ccu.column_name), ', ') || ')' ||
			   CASE WHEN rc.delete_rule != 'NO ACTION' THEN ' ON DELETE ' || rc.delete_rule ELSE '' END ||
			   CASE WHEN rc.update_rule != 'NO ACTION' THEN ' ON UPDATE ' || rc.update_rule ELSE '' END ||
			   ';'
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name
		JOIN information_schema.referential_constraints rc ON tc.constraint_name = rc.constraint_name
		WHERE tc.constraint_type = 'FOREIGN KEY' AND tc.table_schema = 'public'
		GROUP BY tc.table_schema, tc.table_name, tc.constraint_name, ccu.table_schema, ccu.table_name, rc.delete_rule, rc.update_rule`

	rows, err := dtm.source.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var constraints []string
	for rows.Next() {
		var constraintSQL string
		if err := rows.Scan(&constraintSQL); err != nil {
			return nil, err
		}
		constraints = append(constraints, constraintSQL)
	}

	return constraints, rows.Err()
}

// executeSchemaSQL executes schema SQL statements on the destination
func (dtm *DataTransferManager) executeSchemaSQL(schemas []string) error {
	for _, schema := range schemas {
		logrus.Debugf("Executing schema SQL: %s", schema)
		if _, err := dtm.dest.DB.Exec(schema); err != nil {
			// Log but don't fail on constraint errors (they might already exist)
			logrus.Warnf("Schema SQL execution warning: %v", err)
		}
	}
	return nil
}

// filterTables filters tables based on include/exclude configuration
func (dtm *DataTransferManager) filterTables(tables []string) []string {
	if len(dtm.config.IncludeTables) > 0 {
		// If include list is specified, only include those tables
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
		tables = filtered
	}

	if len(dtm.config.ExcludeTables) > 0 {
		// Remove excluded tables
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
		tables = filtered
	}

	return tables
}
