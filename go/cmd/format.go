package cmd

import "github.com/spf13/cobra"

// formatFlag reads the persistent --format flag, defaulting to "table"
// when the root's own default ("text") is in effect, and validates it
// is one of the two shapes this command surface understands. Any other
// value is a usage error.
func formatFlag(cmd *cobra.Command) (string, error) {
	v, err := cmd.Flags().GetString("format")
	if err != nil {
		return "", err
	}
	switch v {
	case "", "text", "table":
		return "table", nil
	case "json":
		return "json", nil
	default:
		return "", newUsageError("unknown --format %q (use table or json)", v)
	}
}
