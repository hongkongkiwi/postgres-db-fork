package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ProcessInfo represents information about a Postgres process
type ProcessInfo struct {
	PID             int     `json:"pid"`
	DatName         string  `json:"datname"`
	UserName        string  `json:"usename"`
	ApplicationName string  `json:"application_name"`
	ClientAddr      *string `json:"client_addr"`
	State           string  `json:"state"`
	Query           string  `json:"query"`
}

// PsResult represents the result of a ps operation
type PsResult struct {
	Format    string        `json:"format"`
	Success   bool          `json:"success"`
	Message   string        `json:"message,omitempty"`
	Error     string        `json:"error,omitempty"`
	Count     int           `json:"count"`
	Processes []ProcessInfo `json:"processes,omitempty"`
	Duration  string        `json:"duration"`
}

// psCmd represents the ps command
var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running Postgres processes",
	Long: `List running processes on a PostgreSQL server.

This command shows information from the pg_stat_activity view.

Examples:
  # List all processes
  postgres-db-fork ps --host localhost --user admin
`,
	RunE: runPs,
}

func init() {
	rootCmd.AddCommand(psCmd)

	// Database connection flags
	psCmd.Flags().String("host", "localhost", "Database server host")
	psCmd.Flags().Int("port", 5432, "Database server port")
	psCmd.Flags().String("user", "", "Database username (required)")
	psCmd.Flags().String("password", "", "Database password")
	psCmd.Flags().String("sslmode", "prefer", "SSL mode")

	// Output options
	psCmd.Flags().String("output-format", "text", "Output format: text or json")
	psCmd.Flags().Bool("quiet", false, "Suppress output except for essential info (or JSON)")

	// Mark required flags
	if err := psCmd.MarkFlagRequired("user"); err != nil {
		panic(fmt.Sprintf("Failed to mark flag as required: %v", err))
	}

	// Bind to viper
	if err := viper.BindPFlag("ps.host", psCmd.Flags().Lookup("host")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("ps.port", psCmd.Flags().Lookup("port")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("ps.user", psCmd.Flags().Lookup("user")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("ps.password", psCmd.Flags().Lookup("password")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("ps.sslmode", psCmd.Flags().Lookup("sslmode")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("ps.output_format", psCmd.Flags().Lookup("output-format")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("ps.quiet", psCmd.Flags().Lookup("quiet")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
}

func runPs(cmd *cobra.Command, args []string) error {
	start := time.Now()

	dbConfig := &config.DatabaseConfig{
		Host:     viper.GetString("ps.host"),
		Port:     viper.GetInt("ps.port"),
		Username: viper.GetString("ps.user"),
		Password: viper.GetString("ps.password"),
		Database: "postgres", // Connect to postgres database for admin operations
		SSLMode:  viper.GetString("ps.sslmode"),
	}
	outputFormat := viper.GetString("ps.output_format")
	quiet := viper.GetBool("ps.quiet")

	conn, err := db.NewConnection(dbConfig)
	if err != nil {
		return outputPsResult(&PsResult{
			Format:  outputFormat,
			Success: false,
			Error:   fmt.Sprintf("Failed to connect to database: %v", err),
		}, quiet)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close connection: %v\n", err)
		}
	}()

	processes, err := getPostgresProcesses(conn)
	if err != nil {
		return outputPsResult(&PsResult{
			Format:  outputFormat,
			Success: false,
			Error:   fmt.Sprintf("Failed to query processes: %v", err),
		}, quiet)
	}

	result := &PsResult{
		Format:    outputFormat,
		Success:   true,
		Count:     len(processes),
		Processes: processes,
		Duration:  time.Since(start).String(),
	}

	if len(processes) == 0 {
		result.Message = "No active processes found"
	} else {
		result.Message = fmt.Sprintf("Found %d processes", len(processes))
	}

	return outputPsResult(result, quiet)
}

func getPostgresProcesses(conn *db.Connection) ([]ProcessInfo, error) {
	query := `
		SELECT pid, datname, usename, application_name, client_addr, state, query
		FROM pg_stat_activity
	`
	rows, err := conn.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing rows: %v\n", err)
		}
	}()

	var processes []ProcessInfo
	for rows.Next() {
		var p ProcessInfo
		if err := rows.Scan(&p.PID, &p.DatName, &p.UserName, &p.ApplicationName, &p.ClientAddr, &p.State, &p.Query); err != nil {
			return nil, err
		}
		processes = append(processes, p)
	}

	return processes, nil
}

func outputPsResult(result *PsResult, quiet bool) error {
	if result.Format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to generate json output: %v", err)
		}
		fmt.Println(string(jsonOutput))
		if !result.Success {
			os.Exit(1)
		}
		return nil
	}

	if !result.Success {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		os.Exit(1)
	}

	if quiet {
		for _, p := range result.Processes {
			fmt.Println(p.PID)
		}
		return nil
	}

	if len(result.Processes) > 0 {
		// Simple text output
		fmt.Printf("%-10s %-20s %-20s %-20s %-15s %-10s %-40s\n", "PID", "DB NAME", "USER", "APPLICATION", "CLIENT", "STATE", "QUERY")
		for _, p := range result.Processes {
			clientAddr := "N/A"
			if p.ClientAddr != nil {
				clientAddr = *p.ClientAddr
			}
			query := p.Query
			if len(query) > 40 {
				query = query[:37] + "..."
			}
			fmt.Printf("%-10d %-20s %-20s %-20s %-15s %-10s %-40s\n", p.PID, p.DatName, p.UserName, p.ApplicationName, clientAddr, p.State, strings.ReplaceAll(query, "\n", " "))
		}
	}

	fmt.Printf("\n%s (%s)\n", result.Message, result.Duration)

	return nil
}
