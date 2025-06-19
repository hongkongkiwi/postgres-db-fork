package cmd

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build information set by linker flags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
	GoVersion = runtime.Version()
)

// VersionInfo represents version and build information
type VersionInfo struct {
	Version      string            `json:"version"`
	GitCommit    string            `json:"git_commit"`
	BuildDate    string            `json:"build_date"`
	GoVersion    string            `json:"go_version"`
	Platform     string            `json:"platform"`
	Arch         string            `json:"arch"`
	Features     []string          `json:"features"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version and build information",
	Long: `Display version information including build details, Go version, and enabled features.

This information is useful for debugging, support requests, and ensuring you're running
the expected version in CI/CD environments.

Examples:
  # Show version information
  postgres-db-fork version

  # JSON output for automation
  postgres-db-fork version --output json

  # Check if specific version
  postgres-db-fork version --output json | jq -r '.version'`,
	RunE: runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)

	versionCmd.Flags().String("output-format", "text", "Output format: text or json")
}

func runVersion(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output-format")

	versionInfo := &VersionInfo{
		Version:      Version,
		GitCommit:    GitCommit,
		BuildDate:    BuildDate,
		GoVersion:    GoVersion,
		Platform:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		Features:     getEnabledFeatures(),
		Dependencies: getDependencies(),
	}

	if outputFormat == "json" {
		jsonOutput, err := json.MarshalIndent(versionInfo, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal version info: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		printVersionText(versionInfo)
	}

	return nil
}

func printVersionText(info *VersionInfo) {
	fmt.Printf("postgres-db-fork version %s\n", info.Version)
	fmt.Printf("Git commit: %s\n", info.GitCommit)
	fmt.Printf("Build date: %s\n", info.BuildDate)
	fmt.Printf("Go version: %s\n", info.GoVersion)
	fmt.Printf("Platform: %s/%s\n", info.Platform, info.Arch)

	if len(info.Features) > 0 {
		fmt.Printf("Features: ")
		for i, feature := range info.Features {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("%s", feature)
		}
		fmt.Println()
	}

	if len(info.Dependencies) > 0 {
		fmt.Println("\nKey Dependencies:")
		for name, version := range info.Dependencies {
			fmt.Printf("  %s: %s\n", name, version)
		}
	}
}

func getEnabledFeatures() []string {
	features := []string{
		"same-server-cloning",
		"cross-server-transfer",
		"template-naming",
		"job-resumption",
		"progress-monitoring",
		"parallel-transfer",
		"cleanup-automation",
		"configuration-validation",
		"json-output",
	}

	// Add conditional features based on build tags or runtime detection
	if runtime.GOOS != "windows" {
		features = append(features, "unix-signals")
	}

	return features
}

func getDependencies() map[string]string {
	// Key dependencies - these would ideally be populated during build
	// For now, we'll include the major ones we know about
	deps := map[string]string{
		"go":     runtime.Version(),
		"lib/pq": "v1.10.9", // PostgreSQL driver
		"cobra":  "v1.8.0",  // CLI framework
		"viper":  "v1.18.2", // Configuration
		"logrus": "v1.9.3",  // Logging
	}

	return deps
}
