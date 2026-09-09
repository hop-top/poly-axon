package cmd

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"hop.top/axon"
)

func init() {
	rootCmd.AddCommand(eventsCmd())
}

func eventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "List the canonical hook event catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			events := axon.Events()
			format, err := formatFlag(cmd)
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(events)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			defer func() { _ = w.Flush() }()
			_, _ = fmt.Fprintln(w, "NAME\tORIGIN\tBLOCKING")
			for _, e := range events {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, e.EffectiveOrigin(), yesNo(e.Blocking))
			}
			return nil
		},
	}
}
