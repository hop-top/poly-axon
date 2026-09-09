package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/axon"
	"hop.top/axon/invoke"
	"hop.top/axon/invoke/all"
)

// adapterFor resolves host (canonical name or alias) to its
// invoke.InvocationAdapter. Unknown host, or a host with no registered
// adapter, is a usage error.
func adapterFor(nameOrAlias string) (invoke.InvocationAdapter, axon.Host, error) {
	h, err := resolveHost(nameOrAlias)
	if err != nil {
		return nil, axon.Host{}, err
	}
	for _, a := range all.Adapters() {
		if a.CLI() == h.Name {
			return a, h, nil
		}
	}
	return nil, axon.Host{}, newUsageError("no invocation adapter for host %q", h.Name)
}

// invokeFlags mirrors kit uxp's commonFlags, minus kitcli-specific
// annotations (see the task report for the list of dropped console
// features).
type invokeFlags struct {
	mode        string
	model       string
	agent       string
	cwd         string
	format      string
	approval    string
	sandbox     string
	files       []string
	images      []string
	addDirs     []string
	configKVs   []string
	extraArgs   []string
	allowDanger bool
	session     string
	cont        bool
	fork        bool
	exec        bool
}

func bindInvokeFlags(cmd *cobra.Command, c *invokeFlags) {
	cmd.Flags().StringVar(&c.mode, "mode", "run", "invocation mode: interactive|run|resume")
	cmd.Flags().StringVar(&c.model, "model", "", "model name (vendor-specific)")
	cmd.Flags().StringVar(&c.agent, "agent", "", "agent / persona / recipe name")
	cmd.Flags().StringVar(&c.cwd, "cwd", "", "working directory for the spawned CLI")
	cmd.Flags().StringVar(&c.format, "output-format", "", "output format: text|json|stream-json (CLI-specific)")
	cmd.Flags().StringVar(&c.approval, "approval", "", "approval mode: ask|auto-edit|auto-all|plan|never")
	cmd.Flags().StringVar(&c.sandbox, "sandbox", "", "sandbox tier: read-only|workspace-write|danger-full-access")
	cmd.Flags().StringSliceVar(&c.files, "file", nil, "file to attach (repeatable)")
	cmd.Flags().StringSliceVar(&c.images, "image", nil, "image to attach (repeatable)")
	cmd.Flags().StringSliceVar(&c.addDirs, "add-dir", nil, "additional workspace directory (repeatable)")
	cmd.Flags().StringArrayVar(&c.configKVs, "config", nil, "Config key=value, namespaced as <cli>.<key> or uxp.<key> (repeatable)")
	cmd.Flags().StringArrayVar(&c.extraArgs, "extra-arg", nil, "raw arg passed to the target CLI (repeatable)")
	cmd.Flags().BoolVar(&c.allowDanger, "allow-dangerous", false, "shorthand for --config uxp.allow_dangerous=true")
	cmd.Flags().StringVar(&c.session, "session", "", "session id to resume")
	cmd.Flags().BoolVar(&c.cont, "continue", false, "resume the most recent session")
	cmd.Flags().BoolVar(&c.fork, "fork", false, "fork the resumed session (only on adapters with native fork)")
	cmd.Flags().BoolVar(&c.exec, "exec", false, "execute the built command instead of just printing argv")
}

func parseInvocation(cliName string, c invokeFlags, prompt string) (invoke.Invocation, error) {
	inv := invoke.Invocation{
		CLI:       cliName,
		Prompt:    prompt,
		Model:     c.model,
		Agent:     c.agent,
		CWD:       c.cwd,
		Files:     c.files,
		Images:    c.images,
		AddDirs:   c.addDirs,
		ExtraArgs: c.extraArgs,
		SessionID: c.session,
		Continue:  c.cont,
		Fork:      c.fork,
	}

	switch c.mode {
	case "", "run":
		inv.Mode = invoke.ModeRun
	case "interactive":
		inv.Mode = invoke.ModeInteractive
	case "resume":
		inv.Mode = invoke.ModeResume
	default:
		return inv, newUsageError("unknown --mode %q (use interactive|run|resume)", c.mode)
	}

	if inv.Mode == invoke.ModeResume && inv.SessionID == "" && !inv.Continue {
		return inv, newUsageError("--session <id> or --continue is required for --mode resume")
	}

	switch c.format {
	case "":
		inv.Output = invoke.OutputDefault
	case "text":
		inv.Output = invoke.OutputText
	case "json":
		inv.Output = invoke.OutputJSON
	case "stream-json":
		inv.Output = invoke.OutputStreamJSON
	default:
		return inv, newUsageError("unknown --output-format %q (use text|json|stream-json)", c.format)
	}

	switch c.approval {
	case "":
	case "ask":
		inv.Approval = invoke.ApprovalAsk
	case "auto-edit":
		inv.Approval = invoke.ApprovalAutoEdit
	case "auto-all":
		inv.Approval = invoke.ApprovalAutoAll
	case "plan":
		inv.Approval = invoke.ApprovalPlan
	case "never":
		inv.Approval = invoke.ApprovalNever
	default:
		return inv, newUsageError("unknown --approval %q", c.approval)
	}

	switch c.sandbox {
	case "":
	case "read-only":
		inv.Sandbox = invoke.SandboxReadOnly
	case "workspace-write":
		inv.Sandbox = invoke.SandboxWorkspaceWrite
	case "danger-full-access":
		inv.Sandbox = invoke.SandboxDangerFullAccess
	default:
		return inv, newUsageError("unknown --sandbox %q", c.sandbox)
	}

	if len(c.configKVs) > 0 {
		inv.Config = make(map[string]string, len(c.configKVs))
		for _, kv := range c.configKVs {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				return inv, newUsageError("--config %q is not key=value", kv)
			}
			inv.Config[k] = v
		}
	}
	if c.allowDanger {
		if inv.Config == nil {
			inv.Config = map[string]string{}
		}
		inv.Config["uxp.allow_dangerous"] = "true"
	}

	return inv, nil
}

func invokeCmd() *cobra.Command {
	var c invokeFlags
	cmd := &cobra.Command{
		Use:   "invoke <host> [-- <prompt words...>]",
		Short: "Build (or execute) a native invocation for a host CLI",
		Long: "Build native argv for a host agent CLI from one normalized " +
			"request. Default prints the built argv to stdout; --exec " +
			"spawns the target CLI directly.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, h, err := adapterFor(args[0])
			if err != nil {
				return err
			}
			prompt := strings.Join(args[1:], " ")
			inv, err := parseInvocation(h.Name, c, prompt)
			if err != nil {
				return err
			}
			return buildAndMaybeExec(cmd, a, inv, c.exec)
		},
	}
	bindInvokeFlags(cmd, &c)
	return cmd
}

func init() {
	rootCmd.AddCommand(invokeCmd())
}

func buildAndMaybeExec(cmd *cobra.Command, a invoke.InvocationAdapter, inv invoke.Invocation, doExec bool) error {
	spec, ds, err := a.Build(inv)
	w := cmd.OutOrStdout()

	// Always print diagnostics (info/warning) to stderr so stdout is
	// reserved for argv (--exec=false) or process output (--exec=true).
	for _, d := range ds {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s: %s\n", d.Level, d.Option, d.Message)
	}
	if err != nil {
		return err
	}

	if !doExec {
		_, _ = fmt.Fprintln(w, shellEscape(append([]string{spec.Path}, spec.Args...)))
		return nil
	}

	osCmd := exec.Command(spec.Path, spec.Args...) //nolint:gosec // invoke is axon's job: run the host CLI the user configured
	osCmd.Dir = spec.Dir
	osCmd.Env = append(os.Environ(), spec.Env...)
	osCmd.Stdin = os.Stdin
	osCmd.Stdout = os.Stdout
	osCmd.Stderr = os.Stderr
	return osCmd.Run()
}

func shellEscape(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if needsQuote(p) {
			out[i] = "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
		} else {
			out[i] = p
		}
	}
	return strings.Join(out, " ")
}

func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '"', '\'', '`', '$', '\\', '|', '&', ';', '<', '>', '(', ')', '[', ']', '{', '}', '*', '?', '~', '!', '#':
			return true
		}
	}
	return false
}
