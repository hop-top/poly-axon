package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/axon/internal/version"

	// Register every built-in hook codec so `fixture`/`validate` work
	// against any host with a codec.
	_ "hop.top/axon/hooks/hosts"
)

var rootCmd = &cobra.Command{
	Use:     "axon",
	Short:   "Host-CLI contract for AI-assistant hooks",
	Version: version.Version(),
	// Cobra prints its own "Error: ..." on RunE failure; keep that off
	// so usageError and runtime errors alike print exactly once via
	// Execute's own os.Stderr write below.
	SilenceErrors: true,
	SilenceUsage:  true,
}

// usageError marks an error as a usage mistake (unknown host, event,
// action, or bad arguments) so Execute exits 2 instead of 1.
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }

// newUsageError wraps err (or a formatted message) as a usageError.
func newUsageError(format string, args ...any) error {
	return usageError{err: fmt.Errorf(format, args...)}
}

// Execute runs the root command and returns the process exit code:
// 0 on success, 2 on a usage error (unknown host/event/action, bad
// args, an unrecognized subcommand/flag, or wrong arg count), 1 on any
// other error.
func Execute() int {
	cmd, err := rootCmd.ExecuteC()
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, err)
	if isUsageError(err) {
		return 2
	}
	// Cobra returns Find/ValidateArgs failures (unknown subcommand,
	// unknown flag, wrong arg count) as plain errors before RunE ever
	// runs; those are usage mistakes too, distinguished here from a
	// RunE-returned runtime error by cmd.RunE being unset (Find failed
	// to resolve a leaf) or by ValidateArgs rejecting the args cobra
	// did resolve.
	if cmd.RunE == nil || cmd.ValidateArgs(cmd.Flags().Args()) != nil {
		return 2
	}
	return 1
}

// isUsageError reports whether err (or something it wraps) is a
// usageError.
func isUsageError(err error) bool {
	for err != nil {
		if _, ok := err.(usageError); ok {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringP(
		"format", "f", "text",
		"Output format (table, json)",
	)
	rootCmd.PersistentFlags().BoolP(
		"verbose", "v", false,
		"Verbose output",
	)
	// Cobra's own flag/arg parse errors (unknown flag, unknown
	// subcommand, wrong arg count) are usage errors: exit 2, not 1.
	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return newUsageError("%s", err)
	})
}

func initConfig() {
	viper.SetEnvPrefix("axon")
	viper.AutomaticEnv()
	home, err := os.UserHomeDir()
	if err == nil {
		viper.AddConfigPath(
			fmt.Sprintf("%s/.config/axon", home),
		)
	}
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	_ = viper.ReadInConfig()
}
