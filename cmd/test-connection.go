package cmd

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ConnectionTestResult represents the result of a connection test
type ConnectionTestResult struct {
	Target          string                `json:"target"`
	Timestamp       time.Time             `json:"timestamp"`
	Overall         string                `json:"overall"` // "success", "warning", "error"
	Tests           map[string]TestResult `json:"tests"`
	Summary         string                `json:"summary,omitempty"`
	Duration        time.Duration         `json:"duration"`
	Recommendations []string              `json:"recommendations,omitempty"`
}

// TestResult represents the result of an individual test
type TestResult struct {
	Status   string        `json:"status"` // "pass", "warn", "fail"
	Duration time.Duration `json:"duration"`
	Message  string        `json:"message"`
	Details  interface{}   `json:"details,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// testConnectionCmd represents the test-connection command
var testConnectionCmd = &cobra.Command{
	Use:   "test-connection",
	Short: "Test database connectivity and troubleshoot connection issues",
	Long: `Comprehensive database connection testing tool that performs multiple checks
to diagnose connectivity, authentication, and permission issues.

This command performs the following tests:
- DNS resolution and network connectivity
- TCP port accessibility
- SSL certificate validation (if applicable)
- Database authentication
- Basic permissions testing
- Connection pool testing

Examples:
  # Test connection using config file
  postgres-db-fork test-connection

  # Test specific database connection
  postgres-db-fork test-connection --host localhost --port 5432 --user myuser --database mydb

  # Test with detailed output
  postgres-db-fork test-connection --verbose

  # JSON output for automation
  postgres-db-fork test-connection --output json

  # Test both source and target from config
  postgres-db-fork test-connection --test-both`,
	RunE: runTestConnection,
}

func init() {
	rootCmd.AddCommand(testConnectionCmd)

	testConnectionCmd.Flags().String("host", "", "Database host")
	testConnectionCmd.Flags().Int("port", 5432, "Database port")
	testConnectionCmd.Flags().String("user", "", "Database user")
	testConnectionCmd.Flags().String("database", "", "Database name")
	testConnectionCmd.Flags().String("sslmode", "", "SSL mode (disable, require, verify-ca, verify-full)")
	testConnectionCmd.Flags().String("output-format", "text", "Output format: text or json")
	testConnectionCmd.Flags().Bool("verbose", false, "Verbose output with detailed diagnostics")
	testConnectionCmd.Flags().Bool("test-both", false, "Test both source and target databases from config")
	testConnectionCmd.Flags().Duration("timeout", 30*time.Second, "Connection timeout")
}

func runTestConnection(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output-format")
	verbose, _ := cmd.Flags().GetBool("verbose")
	testBoth, _ := cmd.Flags().GetBool("test-both")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	if testBoth {
		return runTestBothConnections(outputFormat, verbose, timeout)
	}

	// Get connection parameters
	connParams, err := getConnectionParams(cmd)
	if err != nil {
		return fmt.Errorf("failed to get connection parameters: %w", err)
	}

	// Run the connection test
	result := performConnectionTest(connParams, timeout, verbose)

	// Output results
	return outputConnectionTestResult(result, outputFormat)
}

func runTestBothConnections(outputFormat string, verbose bool, timeout time.Duration) error {
	// Test source connection
	sourceParams := map[string]string{
		"host":     viper.GetString("source.host"),
		"port":     strconv.Itoa(viper.GetInt("source.port")),
		"user":     viper.GetString("source.user"),
		"database": viper.GetString("source.database"),
		"sslmode":  viper.GetString("source.sslmode"),
	}

	// Test target connection
	targetParams := map[string]string{
		"host":     viper.GetString("target.host"),
		"port":     strconv.Itoa(viper.GetInt("target.port")),
		"user":     viper.GetString("target.user"),
		"database": viper.GetString("target.database"),
		"sslmode":  viper.GetString("target.sslmode"),
	}

	if outputFormat == "text" {
		fmt.Println("Testing Source Database Connection:")
		fmt.Println(strings.Repeat("=", 40))
	}

	sourceResult := performConnectionTest(sourceParams, timeout, verbose)
	if err := outputConnectionTestResult(sourceResult, outputFormat); err != nil {
		return err
	}

	if outputFormat == "text" {
		fmt.Println("\nTesting Target Database Connection:")
		fmt.Println(strings.Repeat("=", 40))
	}

	targetResult := performConnectionTest(targetParams, timeout, verbose)
	return outputConnectionTestResult(targetResult, outputFormat)
}

func getConnectionParams(cmd *cobra.Command) (map[string]string, error) {
	params := make(map[string]string)

	// Get from flags first, then fallback to config
	if host, _ := cmd.Flags().GetString("host"); host != "" {
		params["host"] = host
	} else {
		params["host"] = viper.GetString("source.host")
	}

	if port, _ := cmd.Flags().GetInt("port"); cmd.Flags().Changed("port") {
		params["port"] = strconv.Itoa(port)
	} else {
		params["port"] = strconv.Itoa(viper.GetInt("source.port"))
	}

	if user, _ := cmd.Flags().GetString("user"); user != "" {
		params["user"] = user
	} else {
		params["user"] = viper.GetString("source.username")
	}

	if database, _ := cmd.Flags().GetString("database"); database != "" {
		params["database"] = database
	} else {
		params["database"] = viper.GetString("source.database")
	}

	if sslmode, _ := cmd.Flags().GetString("sslmode"); sslmode != "" {
		params["sslmode"] = sslmode
	} else {
		params["sslmode"] = viper.GetString("source.sslmode")
	}

	// Validate required parameters
	if params["host"] == "" {
		return nil, fmt.Errorf("host is required")
	}
	if params["user"] == "" {
		return nil, fmt.Errorf("user is required")
	}
	if params["database"] == "" {
		return nil, fmt.Errorf("database is required")
	}

	// Set defaults
	if params["port"] == "" || params["port"] == "0" {
		params["port"] = "5432"
	}
	if params["sslmode"] == "" {
		params["sslmode"] = "prefer"
	}

	return params, nil
}

func performConnectionTest(params map[string]string, timeout time.Duration, verbose bool) *ConnectionTestResult {
	target := fmt.Sprintf("%s@%s:%s/%s", params["user"], params["host"], params["port"], params["database"])

	result := &ConnectionTestResult{
		Target:    target,
		Timestamp: time.Now(),
		Tests:     make(map[string]TestResult),
		Overall:   "success",
	}

	startTime := time.Now()

	// Test 1: DNS Resolution
	result.Tests["dns"] = testDNSResolution(params["host"], verbose)

	// Test 2: TCP Connectivity
	result.Tests["tcp"] = testTCPConnectivity(params["host"], params["port"], timeout, verbose)

	// Test 3: SSL/TLS (if enabled)
	if params["sslmode"] != "disable" {
		result.Tests["ssl"] = testSSLConnection(params["host"], params["port"], timeout, verbose)
	}

	// Test 4: Database Authentication
	result.Tests["auth"] = testDatabaseAuth(params, timeout, verbose)

	// Test 5: Basic Permissions
	if result.Tests["auth"].Status == "pass" {
		result.Tests["permissions"] = testBasicPermissions(params, timeout, verbose)
	}

	// Test 6: Connection Pool
	if result.Tests["auth"].Status == "pass" {
		result.Tests["pool"] = testConnectionPool(params, timeout, verbose)
	}

	result.Duration = time.Since(startTime)

	// Determine overall status
	result.Overall = determineOverallStatus(result.Tests)
	result.Summary = generateSummary(result.Tests, result.Overall)
	result.Recommendations = generateRecommendations(result.Tests)

	return result
}

func testDNSResolution(host string, verbose bool) TestResult {
	start := time.Now()

	ips, err := net.LookupIP(host)
	duration := time.Since(start)

	if err != nil {
		return TestResult{
			Status:   "fail",
			Duration: duration,
			Message:  "DNS resolution failed",
			Error:    err.Error(),
		}
	}

	details := make([]string, len(ips))
	for i, ip := range ips {
		details[i] = ip.String()
	}

	return TestResult{
		Status:   "pass",
		Duration: duration,
		Message:  fmt.Sprintf("Resolved to %d IP(s)", len(ips)),
		Details:  details,
	}
}

func testTCPConnectivity(host, port string, timeout time.Duration, verbose bool) TestResult {
	start := time.Now()

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	duration := time.Since(start)

	if err != nil {
		return TestResult{
			Status:   "fail",
			Duration: duration,
			Message:  "TCP connection failed",
			Error:    err.Error(),
		}
	}

	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close connection: %v\n", err)
		}
	}()

	return TestResult{
		Status:   "pass",
		Duration: duration,
		Message:  "TCP connection successful",
	}
}

func testSSLConnection(host, port string, timeout time.Duration, verbose bool) TestResult {
	start := time.Now()

	config := &tls.Config{
		ServerName: host,
	}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", net.JoinHostPort(host, port), config)
	duration := time.Since(start)

	if err != nil {
		return TestResult{
			Status:   "warn",
			Duration: duration,
			Message:  "SSL connection failed (may not be required)",
			Error:    err.Error(),
		}
	}

	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close connection: %v\n", err)
		}
	}()

	// Get certificate info
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) > 0 {
		cert := certs[0]
		details := map[string]interface{}{
			"subject":    cert.Subject.String(),
			"issuer":     cert.Issuer.String(),
			"not_after":  cert.NotAfter,
			"not_before": cert.NotBefore,
		}

		return TestResult{
			Status:   "pass",
			Duration: duration,
			Message:  "SSL connection successful",
			Details:  details,
		}
	}

	return TestResult{
		Status:   "pass",
		Duration: duration,
		Message:  "SSL connection successful",
	}
}

func testDatabaseAuth(params map[string]string, timeout time.Duration, verbose bool) TestResult {
	start := time.Now()

	connStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s connect_timeout=%d",
		params["host"], params["port"], params["user"], params["database"], params["sslmode"], int(timeout.Seconds()))

	// Note: We're not including password in connection string for security
	// In a real implementation, you'd get the password securely

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return TestResult{
			Status:   "fail",
			Duration: time.Since(start),
			Message:  "Failed to create connection",
			Error:    err.Error(),
		}
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Printf("Warning: Failed to close database: %v\n", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = db.PingContext(ctx)
	duration := time.Since(start)

	if err != nil {
		return TestResult{
			Status:   "fail",
			Duration: duration,
			Message:  "Database authentication failed",
			Error:    err.Error(),
		}
	}

	return TestResult{
		Status:   "pass",
		Duration: duration,
		Message:  "Database authentication successful",
	}
}

func testBasicPermissions(params map[string]string, timeout time.Duration, verbose bool) TestResult {
	start := time.Now()

	connStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s connect_timeout=%d",
		params["host"], params["port"], params["user"], params["database"], params["sslmode"], int(timeout.Seconds()))

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return TestResult{
			Status:   "fail",
			Duration: time.Since(start),
			Message:  "Failed to create connection",
			Error:    err.Error(),
		}
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Printf("Warning: Failed to close database: %v\n", err)
		}
	}()

	permissions := make(map[string]bool)

	// Test SELECT permission
	_, err = db.Query("SELECT 1")
	permissions["SELECT"] = err == nil

	// Test CREATE permission (try creating a temp table)
	_, err = db.Exec("CREATE TEMP TABLE test_permissions_check (id INT)")
	permissions["CREATE"] = err == nil

	// Test CREATEDB permission
	var canCreateDB bool
	err = db.QueryRow("SELECT useCreateDB FROM pg_user WHERE usename = current_user").Scan(&canCreateDB)
	permissions["CREATEDB"] = err == nil && canCreateDB

	duration := time.Since(start)

	details := permissions
	status := "pass"
	message := "Basic permissions verified"

	if !permissions["SELECT"] {
		status = "fail"
		message = "Missing SELECT permission"
	} else if !permissions["CREATE"] {
		status = "warn"
		message = "Limited CREATE permissions"
	}

	return TestResult{
		Status:   status,
		Duration: duration,
		Message:  message,
		Details:  details,
	}
}

func testConnectionPool(params map[string]string, timeout time.Duration, verbose bool) TestResult {
	start := time.Now()

	connStr := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s connect_timeout=%d",
		params["host"], params["port"], params["user"], params["database"], params["sslmode"], int(timeout.Seconds()))

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return TestResult{
			Status:   "fail",
			Duration: time.Since(start),
			Message:  "Failed to create connection pool",
			Error:    err.Error(),
		}
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Printf("Warning: Failed to close database: %v\n", err)
		}
	}()

	// Set pool settings for testing
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	// Test multiple concurrent connections
	const numConns = 3
	errChan := make(chan error, numConns)

	for i := 0; i < numConns; i++ {
		go func() {
			err := db.Ping()
			errChan <- err
		}()
	}

	var errors []string
	for i := 0; i < numConns; i++ {
		if err := <-errChan; err != nil {
			errors = append(errors, err.Error())
		}
	}

	duration := time.Since(start)

	if len(errors) > 0 {
		return TestResult{
			Status:   "warn",
			Duration: duration,
			Message:  fmt.Sprintf("Connection pool issues (%d/%d failed)", len(errors), numConns),
			Details:  errors,
		}
	}

	return TestResult{
		Status:   "pass",
		Duration: duration,
		Message:  fmt.Sprintf("Connection pool working (%d concurrent connections)", numConns),
	}
}

func determineOverallStatus(tests map[string]TestResult) string {
	hasFailure := false
	hasWarning := false

	for _, test := range tests {
		switch test.Status {
		case "fail":
			hasFailure = true
		case "warn":
			hasWarning = true
		}
	}

	if hasFailure {
		return "error"
	}
	if hasWarning {
		return "warning"
	}
	return "success"
}

func generateSummary(tests map[string]TestResult, overall string) string {
	switch overall {
	case "success":
		return "All connection tests passed successfully"
	case "warning":
		return "Connection successful with some warnings"
	case "error":
		return "Connection failed - see test details"
	default:
		return "Connection test completed"
	}
}

func generateRecommendations(tests map[string]TestResult) []string {
	var recommendations []string

	if tests["dns"].Status == "fail" {
		recommendations = append(recommendations, "Check DNS settings and hostname resolution")
	}

	if tests["tcp"].Status == "fail" {
		recommendations = append(recommendations, "Verify host and port are correct, check firewall settings")
	}

	if tests["ssl"].Status == "fail" {
		recommendations = append(recommendations, "Check SSL configuration or try sslmode=disable for testing")
	}

	if tests["auth"].Status == "fail" {
		recommendations = append(recommendations, "Verify username, password, and database name")
	}

	if tests["permissions"].Status == "fail" {
		recommendations = append(recommendations, "Check user permissions and database access rights")
	}

	if tests["pool"].Status == "warn" {
		recommendations = append(recommendations, "Check connection limits and pool configuration")
	}

	return recommendations
}

func outputConnectionTestResult(result *ConnectionTestResult, outputFormat string) error {
	if outputFormat == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal test result: %w", err)
		}
		fmt.Println(string(jsonOutput))

		// Return error if connection test failed
		if result.Overall == "error" {
			return fmt.Errorf("connection test failed")
		}
		return nil
	}

	// Text output
	statusIcon := getStatusIcon(result.Overall)
	fmt.Printf("%s Connection Test: %s\n", statusIcon, result.Target)
	fmt.Printf("Overall Status: %s\n", result.Overall)
	fmt.Printf("Duration: %v\n", result.Duration.Round(time.Millisecond))
	fmt.Println()

	// Test results
	fmt.Println("Test Results:")
	for testName, test := range result.Tests {
		icon := getTestStatusIcon(test.Status)
		fmt.Printf("  %s %-12s %s (%v)\n", icon, testName+":", test.Message, test.Duration.Round(time.Millisecond))

		if test.Error != "" {
			fmt.Printf("    Error: %s\n", test.Error)
		}
	}

	if len(result.Recommendations) > 0 {
		fmt.Println("\nRecommendations:")
		for _, rec := range result.Recommendations {
			fmt.Printf("  • %s\n", rec)
		}
	}

	fmt.Printf("\n%s\n", result.Summary)

	// Return error if connection test failed
	if result.Overall == "error" {
		return fmt.Errorf("connection test failed")
	}
	return nil
}

func getTestStatusIcon(status string) string {
	switch status {
	case "pass":
		return "✅"
	case "warn":
		return "⚠️"
	case "fail":
		return "❌"
	default:
		return "❓"
	}
}
