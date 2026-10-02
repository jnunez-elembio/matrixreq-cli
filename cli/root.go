package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/VladGavrila/matrixreq-cli/internal/client"
	"github.com/VladGavrila/matrixreq-cli/internal/config"
	"github.com/VladGavrila/matrixreq-cli/internal/service"
	"github.com/VladGavrila/matrixreq-cli/internal/upgrade"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version is set at build time via ldflags.
var Version = "1.6.0"

var (
	flagURL     string
	flagToken   string
	flagOutput  string
	flagProject string
	flagDebug   bool
	flagUpgrade bool
)

var rootCmd = &cobra.Command{
	Use:           "mxreq",
	Short:         "MatrixReq CLI - interact with MatrixALM/MatrixQMS REST API",
	Long:          "mxreq is a command-line tool for managing projects, items, users, and more in MatrixALM/MatrixQMS.",
	Version:       Version,
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		if flagUpgrade {
			if err := upgrade.Run(Version); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			return
		}
		cmd.Help() //nolint:errcheck
	},
}

func init() {
	rootCmd.Flags().BoolVar(&flagUpgrade, "upgrade", false, "upgrade mxreq to the latest release")

	rootCmd.PersistentFlags().StringVar(&flagURL, "url", "", "Matrix instance URL (env: MATRIX_URL)")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "API token (env: MATRIX_TOKEN)")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "table", "Output format: table, json, text")
	rootCmd.PersistentFlags().StringVarP(&flagProject, "project", "p", "", "Project short label (env: MATRIX_DEFAULT_PROJECT)")
	rootCmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Print HTTP request and response details to stderr")

	_ = viper.BindPFlag("url", rootCmd.PersistentFlags().Lookup("url"))
	_ = viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token"))
	_ = viper.BindPFlag("default_project", rootCmd.PersistentFlags().Lookup("project"))
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// getOutputFormat returns the configured output format.
func getOutputFormat() string {
	return flagOutput
}

// requireProject returns the project flag or default project, or errors.
func requireProject() (string, error) {
	if flagProject != "" {
		return flagProject, nil
	}
	cfg, err := config.Load()
	if err == nil && cfg.DefaultProject != "" {
		return cfg.DefaultProject, nil
	}
	return "", fmt.Errorf("project is required (use --project or set default_project in config)")
}

// upperRef uppercases an item ref, folder ref, or category label for the API.
func upperRef(s string) string {
	return strings.ToUpper(s)
}

// categoryFromRef extracts the category short label from a folder ref (e.g. "F-SOFT-1" → "SOFT").
func categoryFromRef(ref string) string {
	if strings.HasPrefix(ref, "F-") {
		rest := ref[2:]
		idx := strings.LastIndex(rest, "-")
		if idx > 0 {
			return rest[:idx]
		}
	}
	idx := strings.Index(ref, "-")
	if idx > 0 {
		return ref[:idx]
	}
	return ref
}

// newService creates a MatrixService from config/flags.
func newService() (*service.MatrixService, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if flagURL != "" {
		cfg.URL = flagURL
	}
	if flagToken != "" {
		cfg.Token = flagToken
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	c := client.New(cfg.URL, cfg.Token, flagDebug)
	return service.New(c), nil
}
