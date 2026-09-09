package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"hop.top/axon"
	"hop.top/axon/hooks"
)

func init() {
	rootCmd.AddCommand(hostsCmd())
}

func hostsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hosts",
		Short: "List every registered host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHostsList(cmd)
		},
	}
	cmd.AddCommand(hostsShowCmd())
	return cmd
}

func runHostsList(cmd *cobra.Command) error {
	hosts := axon.Hosts()
	format, err := formatFlag(cmd)
	if err != nil {
		return err
	}
	if format == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(hosts)
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	defer func() { _ = w.Flush() }()
	writeHostRow(w, "NAME", "STATUS", "HOOKS", "ALIASES", "BINARIES")
	for _, h := range hosts {
		writeHostRow(w,
			h.Name,
			h.Status,
			yesNo(h.Hooks),
			strings.Join(h.Aliases, ","),
			strings.Join(h.Binaries, ","),
		)
	}
	return nil
}

func writeHostRow(w *tabwriter.Writer, name, status, hooks, aliases, binaries string) {
	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, status, hooks, aliases, binaries)
}

func hostsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name-or-alias>",
		Short: "Show the full host record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := resolveHost(args[0])
			if err != nil {
				return err
			}
			format, err := formatFlag(cmd)
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(h)
			}
			// Single-record view: plain "key: value" lines, not
			// tabwriter-aligned, so scripts can grep exact prefixes
			// like "name: claude".
			w := cmd.OutOrStdout()
			fields := [][2]string{
				{"name", h.Name},
				{"status", h.Status},
				{"aliases", strings.Join(h.Aliases, ",")},
				{"binaries", strings.Join(h.Binaries, ",")},
				{"hooks", yesNo(h.Hooks)},
				{"project_key_strategy", h.ProjectKeyStrategy},
				{"envelope_discriminator", h.EnvelopeDiscriminator},
				{"config_file_patterns", strings.Join(h.ConfigFilePatterns, ",")},
				{"hook_config_paths", strings.Join(h.HookConfigPaths, ",")},
			}
			for _, f := range fields {
				_, _ = fmt.Fprintf(w, "%s: %s\n", f[0], f[1])
			}
			return nil
		},
	}
}

// resolveHost resolves a canonical name or alias via axon.Resolve. Every
// command that takes a host argument uses this, not axon.Get, so aliases
// work everywhere. Unknown names are usage errors (exit 2).
func resolveHost(nameOrAlias string) (axon.Host, error) {
	h, ok := axon.Resolve(nameOrAlias)
	if !ok {
		return axon.Host{}, newUsageError("unknown host %q", nameOrAlias)
	}
	return h, nil
}

// codecFor returns the hooks.Codec for a host name, or a usage error
// when the host has no codec registered.
func codecFor(name string) (hooks.Codec, error) {
	c, ok := hooks.For(name)
	if !ok {
		return nil, newUsageError("no codec for host %q", name)
	}
	return c, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
