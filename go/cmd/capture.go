package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"hop.top/axon"
	"hop.top/axon/capture"
	"hop.top/axon/hooks"
)

// defaultCaptureDir is where captures land when --dir is not given. It is
// deliberately outside the repo: a capture is a recording pending review,
// not a tree artifact, and defaulting inside the repo would invite a stray
// `git add .` to commit an unreviewed envelope.
const defaultCaptureDir = ".axon-captures"

func init() {
	rootCmd.AddCommand(captureCmd())
}

func captureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Record real hook envelopes from a host so they can become fixtures",
		Long: strings.TrimSpace(`
Record real hook envelopes from a host CLI.

A capability row says a host raises an event; it does not say what the
host puts on the wire for it. Until an envelope has been recorded, axon
has no verified shape, which is why "axon fixture" refuses those pairs
rather than inventing one. This command group is how a recording is made.

The workflow is four steps, and only the middle two are yours:

  axon capture status              what is still missing
  axon capture install <host>      prints config to add BY HAND
  <use the host normally>          the handler records what arrives
  axon capture status              what arrived, and what to do next

The handler never blocks and never alters a host's behaviour: for every
event it emits that host's own allow response, derived from the host's
codec and checked against the committed decision schemas. Run
"axon capture plan <host>" to see each response and where it came from.

Nothing here writes into spec/. Promoting a reviewed capture to a
committed fixture is a deliberate copy you make.`),
	}
	cmd.PersistentFlags().String("dir", defaultCaptureDir, "Directory captures are written to")
	cmd.AddCommand(captureStatusCmd(), captureInstallCmd(), capturePlanCmd(), captureHandleCmd())
	return cmd
}

// captureDir reads the --dir flag and makes it absolute, so a handler
// invoked by a host from an arbitrary working directory writes where the
// operator meant.
func captureDir(cmd *cobra.Command) (string, error) {
	dir, err := cmd.Flags().GetString("dir")
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", newUsageError("--dir must not be empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func captureStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [host]",
		Short: "Report which host/event pairs still need a captured envelope",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := captureDir(cmd)
			if err != nil {
				return err
			}
			only := ""
			if len(args) == 1 {
				h, err := resolveHost(args[0])
				if err != nil {
					return err
				}
				only = h.Name
			}
			pairs, err := capture.Coverage(dir)
			if err != nil {
				return err
			}
			if only != "" {
				pairs = filterHost(pairs, only)
			}
			format, err := formatFlag(cmd)
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(pairs)
			}
			return writeStatusTable(cmd, dir, pairs)
		},
	}
	cmd.Flags().Bool("missing", false, "List only the pairs with no captured envelope yet")
	return cmd
}

func writeStatusTable(cmd *cobra.Command, dir string, pairs []capture.Pair) error {
	onlyMissing, err := cmd.Flags().GetBool("missing")
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "HOST\tEVENT\tHOST EVENT\tLEVEL\tBLOCKING\tSTATUS\tNEXT")
	shown := 0
	for _, p := range pairs {
		if onlyMissing && p.Status != capture.StatusMissing {
			continue
		}
		shown++
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Host, p.Event, p.HostEvent, p.Level, yesNo(p.Blocking), p.Status, capture.NextStep(p))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fixture, captured, missing := capture.Summarize(pairs)
	_, _ = fmt.Fprintf(out, "\n%d pairs: %d with a committed fixture, %d captured and awaiting review, %d still missing.\n",
		len(pairs), fixture, captured, missing)
	_, _ = fmt.Fprintf(out, "Captures directory: %s\n", dir)
	if captured > 0 {
		_, _ = fmt.Fprintln(out, "\nNext: review a captured file, then copy it to spec/fixtures/hosts/<host>/<Event>.input.json")
		_, _ = fmt.Fprintln(out, "and write spec/hosts/<host>/hooks/<Event>.input.schema.json from it. See docs/capturing-envelopes.md.")
	} else if missing > 0 {
		_, _ = fmt.Fprintf(out, "\nNext: axon capture install <host> --dir %s\n", dir)
	}
	if shown == 0 {
		_, _ = fmt.Fprintln(out, "\n(no rows matched)")
	}
	return nil
}

func filterHost(pairs []capture.Pair, host string) []capture.Pair {
	var out []capture.Pair
	for _, p := range pairs {
		if p.Host == host {
			out = append(out, p)
		}
	}
	return out
}

func captureInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <host>",
		Short: "Print the configuration to add to a host so it calls the capture handler",
		Long: strings.TrimSpace(`
Print the hook configuration for a host. It is printed, never written:
the settings file belongs to whoever runs the host, it holds their
unrelated configuration, and installing a hook is their decision.

The command prints the candidate settings paths from the host's own
spec/hosts/<host>/host.yaml hook_config_paths, and a body subscribing to
the host-side event names its capabilities.yaml lists — by default only
the events with no committed fixture yet.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := resolveHost(args[0])
			if err != nil {
				return err
			}
			dir, err := captureDir(cmd)
			if err != nil {
				return err
			}
			all, err := cmd.Flags().GetBool("all")
			if err != nil {
				return err
			}
			s, err := capture.SnippetFor(h.Name, handlerArgv(h.Name, dir), all)
			if err != nil {
				return newUsageError("%s", err)
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "# %s capture handler\n\n", s.Host)
			_, _ = fmt.Fprintln(out, "Add the body below to ONE of these files (from spec/hosts/"+s.Host+"/host.yaml hook_config_paths):")
			for _, p := range s.ConfigPaths {
				_, _ = fmt.Fprintf(out, "  %s\n", p)
			}
			_, _ = fmt.Fprintf(out, "\nSubscribes to %d host event(s): %s\n", len(s.HostEvents), strings.Join(s.HostEvents, ", "))
			if s.Warning != "" {
				_, _ = fmt.Fprintf(out, "\nNote: %s\n", s.Warning)
			}
			_, _ = fmt.Fprintf(out, "\n```%s\n%s\n```\n", s.Language, s.Body)
			_, _ = fmt.Fprintf(out, "\nCaptures land in %s. Remove the block above when you are done capturing.\n", dir)
			_, _ = fmt.Fprintf(out, "Check what the handler answers each event with: axon capture plan %s\n", s.Host)
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "Subscribe to every event the host raises, not only the ones missing a fixture")
	return cmd
}

// handlerArgv builds the argument vector a host is configured to run. The
// host name is part of it because `capture handle` requires it: the
// envelope names the event, but only the configuration knows which host
// sent it, and the host is what selects the codec that reads it.
//
// It returns an argv rather than a command line so a binary path or a
// capture directory containing a space survives; SnippetFor quotes it for
// the hosts whose settings file wants a single string.
//
// os.Executable is preferred over the bare name so a host started from an
// arbitrary working directory, with an arbitrary PATH, still finds the
// same binary the operator ran this command with.
func handlerArgv(host, dir string) []string {
	bin := "axon"
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			bin = resolved
		} else {
			bin = exe
		}
	}
	return []string{bin, "capture", "handle", host, "--dir", dir}
}

func capturePlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan <host>",
		Short: "Show the pass-through response the handler emits for each of a host's events",
		Long: strings.TrimSpace(`
Show, for every event a host raises, exactly what the capture handler
writes back and why.

This is the safety surface: a hook that answers a blocking event wrongly
stops a tool call, a prompt or a turn. Every response below comes from
the host's own codec, and every response for a pair the spec ships an
allow schema for is checked against that schema by the capture package's
own tests.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := resolveHost(args[0])
			if err != nil {
				return err
			}
			// Resolve the codec first so a host with no codec is a usage
			// error naming that, rather than an empty table.
			if _, err := codecFor(h.Name); err != nil {
				return err
			}
			pairs, err := capture.Coverage("")
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			defer func() { _ = w.Flush() }()
			_, _ = fmt.Fprintln(w, "EVENT\tBLOCKING\tSTDOUT\tEXIT\tDERIVED FROM")
			for _, p := range filterHost(pairs, h.Name) {
				pt, err := capture.PassThroughFor(h.Name, p.Event)
				if err != nil {
					return err
				}
				stdout := string(pt.Stdout)
				if stdout == "" {
					stdout = "(nothing)"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", p.Event, yesNo(p.Blocking), stdout, pt.Exit, pt.Source)
			}
			return nil
		},
	}
}

func captureHandleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "handle <host>",
		Short: "Read one hook envelope on stdin, record it, and answer the host with its allow response",
		Long: strings.TrimSpace(`
The handler itself. A host runs this; a person rarely does.

It reads one envelope from stdin, works out which event it is by decoding
it with the host's codec, writes the normalized envelope to
<dir>/<host>/<Event>.input.json, and answers the host with that host's own
allow response.

Recording is best-effort and answering is not: if anything goes wrong
while recording, the handler still emits the allow response and exits
with the host's allow code, because a capture harness must never be the
reason a session stalls. Recording failures go to stderr.`),
		// ExactArgs, matching install and plan: cobra rejects a wrong arg
		// count before RunE and Execute maps that to exit 2, so there is
		// one path for it rather than two that could disagree.
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHandle(cmd, args)
		},
	}
}

// runHandle is the handler body.
//
// Its one hard contract: once a host name is known, this function exits
// with that host's allow code and nothing else. Every failure past that
// point — an unreadable stdin, an envelope no codec can place, a capture
// directory that cannot be written — is reported on stderr and answered
// with allow, because a handler installed to watch must never be the
// reason a tool call, a prompt or a turn is denied.
//
// The only exit-2 paths are the ones that run BEFORE a host is known: a
// missing or unknown host argument, or an empty --dir. Those happen when a
// person runs the command by hand, never when a configured host invokes
// it.
func runHandle(cmd *cobra.Command, args []string) error {
	h, err := resolveHost(args[0])
	if err != nil {
		return err
	}
	codec, err := codecFor(h.Name)
	if err != nil {
		return err
	}
	dir, err := captureDir(cmd)
	if err != nil {
		return err
	}

	// From here on the host may be waiting on this process, so every path
	// ends in allow.
	raw, readErr := io.ReadAll(cmd.InOrStdin())
	event, decodeErr := axon.Event(""), error(nil)
	if readErr == nil {
		in, err := codec.DecodeInput(raw)
		event, decodeErr = in.Event, err
	}

	// Answer FIRST, so nothing below can delay or change what the host
	// reads. A blank event means the envelope could not be placed; the
	// codec's own default shape for a blank Decision.Event is that host's
	// tool-gate allow, which is the safest answer available without
	// knowing which event arrived.
	pt := passThroughOrDefault(cmd, h.Name, event, decodeErr)
	if len(pt.Stdout) > 0 {
		_, _ = cmd.OutOrStdout().Write(pt.Stdout)
	}

	switch {
	case readErr != nil:
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "axon capture: reading the envelope: %v\n", readErr)
	case decodeErr != nil:
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "axon capture: cannot tell which event this envelope is, so nothing was recorded: %v\n", decodeErr)
	default:
		if rec, err := capture.Write(h.Name, dir, raw); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "axon capture: recording: %v\n", err)
		} else {
			reportRecorded(cmd, rec)
		}
	}
	if pt.Exit != 0 {
		// No registered host's allow code is non-zero today, so this is
		// unreachable against the current spec; it is here so a future
		// host with a non-zero allow code is honoured rather than
		// silently answered with 0.
		//
		// os.Exit runs no deferred function and flushes no buffer, and
		// this branch exists precisely to deliver BOTH halves of the
		// answer, so flush the response first. Losing the body while
		// still setting the code would hand the host half a decision.
		if f, ok := cmd.OutOrStdout().(interface{ Flush() error }); ok {
			_ = f.Flush()
		}
		os.Exit(pt.Exit)
	}
	return nil
}

// passThroughOrDefault returns the host's allow response for event, or —
// when the envelope could not be placed — that host's allow response for
// its default decision shape (a blank Decision.Event, which every codec
// documents as its tool-gate default).
//
// It never returns an error, and never a non-allow exit code: a handler
// with nothing useful to say still has to say allow.
func passThroughOrDefault(cmd *cobra.Command, host string, event axon.Event, decodeErr error) capture.PassThrough {
	if decodeErr == nil && event != "" {
		pt, err := capture.PassThroughFor(host, event)
		if err == nil {
			return pt
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "axon capture: %v\n", err)
	}
	h, _ := axon.Get(host)
	codec, ok := hooks.For(host)
	if !ok {
		return capture.PassThrough{Exit: h.ExitCodes.Allow, Source: capture.SourceSilent}
	}
	// A blank Event is the codec's documented "host default shape" input.
	out, exit, err := codec.EncodeDecision(hooks.Decision{Action: hooks.ActionAllow})
	if err != nil || exit != h.ExitCodes.Allow {
		// Fall back to silence at the allow code, which every registered
		// codec's DecodeDecision reads as allow.
		return capture.PassThrough{Exit: h.ExitCodes.Allow, Source: capture.SourceSilent}
	}
	return capture.PassThrough{Stdout: out, Exit: exit, Source: capture.SourceCodec}
}

func reportRecorded(cmd *cobra.Command, rec capture.Record) {
	w := cmd.ErrOrStderr()
	switch {
	case !rec.Replaced:
		_, _ = fmt.Fprintf(w, "axon capture: recorded %s/%s -> %s\n", rec.Host, rec.Event, rec.Path)
	case rec.Changed:
		_, _ = fmt.Fprintf(w, "axon capture: re-recorded %s/%s (content CHANGED) -> %s\n", rec.Host, rec.Event, rec.Path)
	default:
		_, _ = fmt.Fprintf(w, "axon capture: %s/%s unchanged -> %s\n", rec.Host, rec.Event, rec.Path)
	}
}
