package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

// Profile represents a configuration profile
type Profile struct {
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	Config      map[string]interface{} `yaml:"config" json:"config"`
	CreatedAt   time.Time              `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time              `yaml:"updated_at" json:"updated_at"`
	Tags        []string               `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// ProfileStore manages configuration profiles
type ProfileStore struct {
	ProfilesDir string
	Profiles    map[string]Profile
}

// profileCmd represents the profile command
var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage configuration profiles",
	Long: `Manage named configuration profiles for different environments (dev, staging, prod).

Profiles allow you to save and reuse database connection settings and transfer
configurations for different environments, making it easy to switch between
development, staging, and production setups.

Available subcommands:
  list    - List all saved profiles
  create  - Create a new profile
  show    - Display profile details
  use     - Apply a profile configuration
  delete  - Remove a profile
  export  - Export profiles for sharing
  import  - Import profiles from file

Examples:
  # List all profiles
  postgres-db-fork profile list

  # Create a new profile for development
  postgres-db-fork profile create dev --description "Development environment"

  # Show profile details
  postgres-db-fork profile show dev

  # Use a profile
  postgres-db-fork profile use production

  # Export profiles for backup
  postgres-db-fork profile export --output profiles-backup.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration profiles",
	RunE:  runProfileList,
}

var profileCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new configuration profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileCreate,
}

var profileShowCmd = &cobra.Command{
	Use:   "show [name]",
	Short: "Show profile details",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileShow,
}

var profileUseCmd = &cobra.Command{
	Use:   "use [name]",
	Short: "Apply a profile configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileUse,
}

var profileDeleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileDelete,
}

var profileExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export profiles to file",
	RunE:  runProfileExport,
}

var profileImportCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import profiles from file",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileImport,
}

func init() {
	rootCmd.AddCommand(profileCmd)

	// Add subcommands
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileCreateCmd)
	profileCmd.AddCommand(profileShowCmd)
	profileCmd.AddCommand(profileUseCmd)
	profileCmd.AddCommand(profileDeleteCmd)
	profileCmd.AddCommand(profileExportCmd)
	profileCmd.AddCommand(profileImportCmd)

	// Profile create flags
	profileCreateCmd.Flags().String("description", "", "Profile description")
	profileCreateCmd.Flags().StringSlice("tags", nil, "Profile tags")
	profileCreateCmd.Flags().Bool("from-current", false, "Create profile from current configuration")

	// Profile list flags
	profileListCmd.Flags().String("output-format", "table", "Output format: table, json, yaml")
	profileListCmd.Flags().String("tag", "", "Filter by tag")

	// Profile show flags
	profileShowCmd.Flags().String("output-format", "yaml", "Output format: yaml, json")

	// Profile export flags
	profileExportCmd.Flags().String("output", "profiles.yaml", "Output file")
	profileExportCmd.Flags().String("format", "yaml", "Export format: yaml, json")
	profileExportCmd.Flags().StringSlice("profiles", nil, "Specific profiles to export (default: all)")

	// Profile use flags
	profileUseCmd.Flags().Bool("merge", false, "Merge with current config instead of replacing")
}

func getProfileStore() (*ProfileStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	profilesDir := filepath.Join(homeDir, ".postgres-db-fork", "profiles")
	if err := os.MkdirAll(profilesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create profiles directory: %w", err)
	}

	store := &ProfileStore{
		ProfilesDir: profilesDir,
		Profiles:    make(map[string]Profile),
	}

	// Load existing profiles
	if err := store.loadProfiles(); err != nil {
		return nil, fmt.Errorf("failed to load profiles: %w", err)
	}

	return store, nil
}

func (ps *ProfileStore) loadProfiles() error {
	files, err := os.ReadDir(ps.ProfilesDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".yaml") && !strings.HasSuffix(file.Name(), ".yml") {
			continue
		}

		profilePath := filepath.Join(ps.ProfilesDir, file.Name())
		data, err := os.ReadFile(profilePath)
		if err != nil {
			continue // Skip unreadable files
		}

		var profile Profile
		if err := yaml.Unmarshal(data, &profile); err != nil {
			continue // Skip invalid files
		}

		ps.Profiles[profile.Name] = profile
	}

	return nil
}

func (ps *ProfileStore) saveProfile(profile Profile) error {
	profilePath := filepath.Join(ps.ProfilesDir, profile.Name+".yaml")

	data, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("failed to marshal profile: %w", err)
	}

	if err := os.WriteFile(profilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write profile file: %w", err)
	}

	ps.Profiles[profile.Name] = profile
	return nil
}

func (ps *ProfileStore) deleteProfile(name string) error {
	profilePath := filepath.Join(ps.ProfilesDir, name+".yaml")

	if err := os.Remove(profilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete profile file: %w", err)
	}

	delete(ps.Profiles, name)
	return nil
}

func runProfileList(cmd *cobra.Command, args []string) error {
	store, err := getProfileStore()
	if err != nil {
		return err
	}

	outputFormat, _ := cmd.Flags().GetString("output-format")
	tagFilter, _ := cmd.Flags().GetString("tag")

	// Filter by tag if specified
	var profiles []Profile
	for _, profile := range store.Profiles {
		if tagFilter != "" {
			found := false
			for _, tag := range profile.Tags {
				if tag == tagFilter {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		profiles = append(profiles, profile)
	}

	switch outputFormat {
	case "json":
		return outputProfilesJSON(profiles)
	case "yaml":
		return outputProfilesYAML(profiles)
	default:
		return outputProfilesTable(profiles)
	}
}

func runProfileCreate(cmd *cobra.Command, args []string) error {
	store, err := getProfileStore()
	if err != nil {
		return err
	}

	name := args[0]
	description, _ := cmd.Flags().GetString("description")
	tags, _ := cmd.Flags().GetStringSlice("tags")
	fromCurrent, _ := cmd.Flags().GetBool("from-current")

	// Check if profile already exists
	if _, exists := store.Profiles[name]; exists {
		return fmt.Errorf("profile '%s' already exists", name)
	}

	profile := Profile{
		Name:        name,
		Description: description,
		Config:      make(map[string]interface{}),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Tags:        tags,
	}

	if fromCurrent {
		// Capture current viper configuration
		profile.Config = viper.AllSettings()
	} else {
		// Create empty profile with basic structure
		profile.Config = map[string]interface{}{
			"source": map[string]interface{}{
				"host":     "",
				"port":     5432,
				"user":     "",
				"database": "",
				"sslmode":  "prefer",
			},
			"target": map[string]interface{}{
				"host":     "",
				"port":     5432,
				"user":     "",
				"database": "",
				"sslmode":  "prefer",
			},
		}
	}

	if err := store.saveProfile(profile); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	fmt.Printf("✅ Created profile '%s'\n", name)
	if description != "" {
		fmt.Printf("   Description: %s\n", description)
	}
	if len(tags) > 0 {
		fmt.Printf("   Tags: %s\n", strings.Join(tags, ", "))
	}

	return nil
}

func runProfileShow(cmd *cobra.Command, args []string) error {
	store, err := getProfileStore()
	if err != nil {
		return err
	}

	name := args[0]
	outputFormat, _ := cmd.Flags().GetString("output-format")

	profile, exists := store.Profiles[name]
	if !exists {
		return fmt.Errorf("profile '%s' not found", name)
	}

	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(profile, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal profile: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(profile)
		if err != nil {
			return fmt.Errorf("failed to marshal profile: %w", err)
		}
		fmt.Print(string(data))
	}

	return nil
}

func runProfileUse(cmd *cobra.Command, args []string) error {
	store, err := getProfileStore()
	if err != nil {
		return err
	}

	name := args[0]
	merge, _ := cmd.Flags().GetBool("merge")

	profile, exists := store.Profiles[name]
	if !exists {
		return fmt.Errorf("profile '%s' not found", name)
	}

	if !merge {
		// Clear current configuration
		for key := range viper.AllSettings() {
			viper.Set(key, nil)
		}
	}

	// Apply profile configuration
	for key, value := range profile.Config {
		viper.Set(key, value)
	}

	fmt.Printf("✅ Applied profile '%s'\n", name)
	if profile.Description != "" {
		fmt.Printf("   %s\n", profile.Description)
	}

	return nil
}

func runProfileDelete(cmd *cobra.Command, args []string) error {
	store, err := getProfileStore()
	if err != nil {
		return err
	}

	name := args[0]

	if _, exists := store.Profiles[name]; !exists {
		return fmt.Errorf("profile '%s' not found", name)
	}

	if err := store.deleteProfile(name); err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	fmt.Printf("✅ Deleted profile '%s'\n", name)
	return nil
}

func runProfileExport(cmd *cobra.Command, args []string) error {
	store, err := getProfileStore()
	if err != nil {
		return err
	}

	outputFile, _ := cmd.Flags().GetString("output")
	format, _ := cmd.Flags().GetString("format")
	profileNames, _ := cmd.Flags().GetStringSlice("profiles")

	// Select profiles to export
	var profilesToExport []Profile
	if len(profileNames) > 0 {
		for _, name := range profileNames {
			if profile, exists := store.Profiles[name]; exists {
				profilesToExport = append(profilesToExport, profile)
			}
		}
	} else {
		for _, profile := range store.Profiles {
			profilesToExport = append(profilesToExport, profile)
		}
	}

	var data []byte
	switch format {
	case "json":
		data, err = json.MarshalIndent(profilesToExport, "", "  ")
	case "yaml":
		data, err = yaml.Marshal(profilesToExport)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal profiles: %w", err)
	}

	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	fmt.Printf("✅ Exported %d profiles to %s\n", len(profilesToExport), outputFile)
	return nil
}

func runProfileImport(cmd *cobra.Command, args []string) error {
	store, err := getProfileStore()
	if err != nil {
		return err
	}

	inputFile := args[0]

	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read import file: %w", err)
	}

	var profiles []Profile

	// Try YAML first, then JSON
	if err := yaml.Unmarshal(data, &profiles); err != nil {
		if err := json.Unmarshal(data, &profiles); err != nil {
			return fmt.Errorf("failed to parse import file (tried YAML and JSON): %w", err)
		}
	}

	imported := 0
	for _, profile := range profiles {
		profile.UpdatedAt = time.Now()
		if err := store.saveProfile(profile); err != nil {
			fmt.Printf("⚠️  Failed to import profile '%s': %v\n", profile.Name, err)
			continue
		}
		imported++
	}

	fmt.Printf("✅ Imported %d profiles from %s\n", imported, inputFile)
	return nil
}

func outputProfilesTable(profiles []Profile) error {
	if len(profiles) == 0 {
		fmt.Println("No profiles found.")
		return nil
	}

	fmt.Printf("%-20s %-30s %-15s %-20s\n", "NAME", "DESCRIPTION", "TAGS", "UPDATED")
	fmt.Printf("%s\n", strings.Repeat("-", 85))

	for _, profile := range profiles {
		tags := strings.Join(profile.Tags, ",")
		if len(tags) > 15 {
			tags = tags[:12] + "..."
		}

		description := profile.Description
		if len(description) > 30 {
			description = description[:27] + "..."
		}

		fmt.Printf("%-20s %-30s %-15s %-20s\n",
			profile.Name,
			description,
			tags,
			profile.UpdatedAt.Format("2006-01-02 15:04"))
	}

	return nil
}

func outputProfilesJSON(profiles []Profile) error {
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profiles: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func outputProfilesYAML(profiles []Profile) error {
	data, err := yaml.Marshal(profiles)
	if err != nil {
		return fmt.Errorf("failed to marshal profiles: %w", err)
	}
	fmt.Print(string(data))
	return nil
}
