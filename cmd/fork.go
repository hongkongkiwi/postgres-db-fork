package cmd

import (
	"context"
	"fmt"
	"time"

	"postgres-db-fork/internal/config"
	"postgres-db-fork/internal/fork"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// forkCmd represents the fork command
var forkCmd = &cobra.Command{
	Use:   "fork",
	Short: "Fork a PostgreSQL database",
	Long: `Fork (copy) a PostgreSQL database from source to destination.

The tool supports two modes:
1. Same-server forking: When source and destination are on the same PostgreSQL server,
   uses efficient template-based cloning.
2. Cross-server forking: When source and destination are on different servers,
   uses dump and restore operations with parallel data transfer.

Examples:
  # Fork database within same server
  postgres-db-fork fork --source-host localhost --source-db myapp_prod --target-db myapp_dev

  # Fork database to different server  
  postgres-db-fork fork --source-host prod.example.com --source-db myapp \
                        --dest-host dev.example.com --target-db myapp_copy

  # Fork with configuration file
  postgres-db-fork fork --config fork-config.yaml`,
	RunE: runFork,
}

func init() {
	rootCmd.AddCommand(forkCmd)

	// Source database flags
	forkCmd.Flags().String("source-host", "localhost", "Source database host")
	forkCmd.Flags().Int("source-port", 5432, "Source database port")
	forkCmd.Flags().String("source-user", "", "Source database username")
	forkCmd.Flags().String("source-password", "", "Source database password")
	forkCmd.Flags().String("source-db", "", "Source database name (required)")
	forkCmd.Flags().String("source-sslmode", "prefer", "Source database SSL mode")

	// Destination database flags
	forkCmd.Flags().String("dest-host", "", "Destination database host (defaults to source-host)")
	forkCmd.Flags().Int("dest-port", 0, "Destination database port (defaults to source-port)")
	forkCmd.Flags().String("dest-user", "", "Destination database username (defaults to source-user)")
	forkCmd.Flags().String("dest-password", "", "Destination database password (defaults to source-password)")
	forkCmd.Flags().String("dest-sslmode", "", "Destination database SSL mode (defaults to source-sslmode)")

	// Target database
	forkCmd.Flags().String("target-db", "", "Target database name (required)")

	// Fork options
	forkCmd.Flags().Bool("drop-if-exists", false, "Drop target database if it exists")
	forkCmd.Flags().Int("max-connections", 4, "Maximum number of parallel connections for data transfer")
	forkCmd.Flags().Int("chunk-size", 1000, "Number of rows to transfer in each batch")
	forkCmd.Flags().Duration("timeout", 30*time.Minute, "Operation timeout")
	forkCmd.Flags().StringSlice("exclude-tables", []string{}, "Tables to exclude from transfer")
	forkCmd.Flags().StringSlice("include-tables", []string{}, "Tables to include in transfer (if specified, only these tables will be transferred)")
	forkCmd.Flags().Bool("schema-only", false, "Transfer schema only (no data)")
	forkCmd.Flags().Bool("data-only", false, "Transfer data only (no schema)")

	// Mark required flags
	forkCmd.MarkFlagRequired("source-db")
	forkCmd.MarkFlagRequired("target-db")

	// Bind flags to viper
	viper.BindPFlag("source.host", forkCmd.Flags().Lookup("source-host"))
	viper.BindPFlag("source.port", forkCmd.Flags().Lookup("source-port"))
	viper.BindPFlag("source.username", forkCmd.Flags().Lookup("source-user"))
	viper.BindPFlag("source.password", forkCmd.Flags().Lookup("source-password"))
	viper.BindPFlag("source.database", forkCmd.Flags().Lookup("source-db"))
	viper.BindPFlag("source.sslmode", forkCmd.Flags().Lookup("source-sslmode"))

	viper.BindPFlag("destination.host", forkCmd.Flags().Lookup("dest-host"))
	viper.BindPFlag("destination.port", forkCmd.Flags().Lookup("dest-port"))
	viper.BindPFlag("destination.username", forkCmd.Flags().Lookup("dest-user"))
	viper.BindPFlag("destination.password", forkCmd.Flags().Lookup("dest-password"))
	viper.BindPFlag("destination.sslmode", forkCmd.Flags().Lookup("dest-sslmode"))

	viper.BindPFlag("target_database", forkCmd.Flags().Lookup("target-db"))
	viper.BindPFlag("drop_if_exists", forkCmd.Flags().Lookup("drop-if-exists"))
	viper.BindPFlag("max_connections", forkCmd.Flags().Lookup("max-connections"))
	viper.BindPFlag("chunk_size", forkCmd.Flags().Lookup("chunk-size"))
	viper.BindPFlag("timeout", forkCmd.Flags().Lookup("timeout"))
	viper.BindPFlag("exclude_tables", forkCmd.Flags().Lookup("exclude-tables"))
	viper.BindPFlag("include_tables", forkCmd.Flags().Lookup("include-tables"))
	viper.BindPFlag("schema_only", forkCmd.Flags().Lookup("schema-only"))
	viper.BindPFlag("data_only", forkCmd.Flags().Lookup("data-only"))
}

func runFork(cmd *cobra.Command, args []string) error {
	// Build configuration from flags and config file
	cfg := &config.ForkConfig{}

	// Source configuration
	cfg.Source.Host = viper.GetString("source.host")
	cfg.Source.Port = viper.GetInt("source.port")
	cfg.Source.Username = viper.GetString("source.username")
	cfg.Source.Password = viper.GetString("source.password")
	cfg.Source.Database = viper.GetString("source.database")
	cfg.Source.SSLMode = viper.GetString("source.sslmode")

	// Destination configuration - default to source values if not specified
	cfg.Destination.Host = viper.GetString("destination.host")
	if cfg.Destination.Host == "" {
		cfg.Destination.Host = cfg.Source.Host
	}

	cfg.Destination.Port = viper.GetInt("destination.port")
	if cfg.Destination.Port == 0 {
		cfg.Destination.Port = cfg.Source.Port
	}

	cfg.Destination.Username = viper.GetString("destination.username")
	if cfg.Destination.Username == "" {
		cfg.Destination.Username = cfg.Source.Username
	}

	cfg.Destination.Password = viper.GetString("destination.password")
	if cfg.Destination.Password == "" {
		cfg.Destination.Password = cfg.Source.Password
	}

	cfg.Destination.SSLMode = viper.GetString("destination.sslmode")
	if cfg.Destination.SSLMode == "" {
		cfg.Destination.SSLMode = cfg.Source.SSLMode
	}

	// Other configuration
	cfg.TargetDatabase = viper.GetString("target_database")
	cfg.DropIfExists = viper.GetBool("drop_if_exists")
	cfg.MaxConnections = viper.GetInt("max_connections")
	cfg.ChunkSize = viper.GetInt("chunk_size")
	cfg.Timeout = viper.GetDuration("timeout")
	cfg.ExcludeTables = viper.GetStringSlice("exclude_tables")
	cfg.IncludeTables = viper.GetStringSlice("include_tables")
	cfg.SchemaOnly = viper.GetBool("schema_only")
	cfg.DataOnly = viper.GetBool("data_only")

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Validate conflicting options
	if cfg.SchemaOnly && cfg.DataOnly {
		return fmt.Errorf("cannot specify both --schema-only and --data-only")
	}

	// Create forker and execute
	forker := fork.NewForker(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	return forker.Fork(ctx)
}
