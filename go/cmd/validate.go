package cmd

import (
	"io"
	"os"

	"github.com/spf13/cobra"
	"hop.top/axon"
	"hop.top/axon/hooks"
)

func init() {
	rootCmd.AddCommand(validateCmd())
}

func validateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a hook payload against a host's JSON schema",
	}
	cmd.AddCommand(validateInputCmd(), validateDecisionCmd())
	return cmd
}

// readPayload reads path, or stdin when path is "-".
func readPayload(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path) //nolint:gosec // reading a user-supplied CLI file argument is the command's purpose
}

func validateInputCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "input <host> <event> <file|->",
		Short: "Validate a hook input payload",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := resolveHost(args[0])
			if err != nil {
				return err
			}
			raw, err := readPayload(cmd, args[2])
			if err != nil {
				return err
			}
			return hooks.ValidateInput(h.Name, axon.Event(args[1]), raw)
		},
	}
}

func validateDecisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "decision <host> <event> <action> <file|->",
		Short: "Validate a hook decision payload",
		Args:  cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := resolveHost(args[0])
			if err != nil {
				return err
			}
			raw, err := readPayload(cmd, args[3])
			if err != nil {
				return err
			}
			return hooks.ValidateDecision(h.Name, axon.Event(args[1]), hooks.Action(args[2]), raw)
		},
	}
}
