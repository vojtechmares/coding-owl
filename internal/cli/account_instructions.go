package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// maxInstructions bounds what is read as an Account's standing instructions,
// so a mistaken `owl account instructions set work < some-huge-file` is
// refused rather than held in memory. The daemon bounds them too; this is so
// the refusal happens before the whole file is read.
const maxInstructions = 64 << 10

// noInstructions is what is printed for an Account nobody has written any for.
const noInstructions = "no standing instructions yet"

// fallbackEditor is what owl account instructions edit opens when neither
// VISUAL nor EDITOR says otherwise. It is on every machine Owl runs on.
const fallbackEditor = "vi"

func newAccountInstructionsCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instructions",
		Short: "Read and write the standing instructions an Account's Runs follow",
		Long: `Read and write an Account's standing instructions.

Every Run on an Account reads them, whichever Project the Run is for. They are
the place for conventions that hold across all of an Account's work - how to
write a commit message, what never to touch - as against a Project's own
unattendedClauses, which hold only for that Project.

Owl keeps them in the Account's configuration directory, under whatever its
coding tool calls that file, so the tool reads them itself and Owl injects
nothing.`,
	}
	cmd.AddCommand(newInstructionsShowCmd(env), newInstructionsSetCmd(env), newInstructionsEditCmd(env))
	return cmd
}

func newInstructionsShowCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Print an Account's standing instructions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				in, err := c.AccountInstructions(ctx, args[0])
				if err != nil {
					return err
				}
				if err := readable(in); err != nil {
					return err
				}
				// Where they are goes to standard error, so that standard
				// output is the instructions and nothing else: the README
				// puts this command beside `set`, and somebody who pipes one
				// into the other should not find two lines of Owl's own
				// prose at the top of their file.
				_, _ = fmt.Fprintf(env.Stderr, "%s\n", in.Path)
				if strings.TrimSpace(in.Text) == "" {
					_, _ = fmt.Fprintln(env.Stderr, noInstructions)
					return nil
				}
				// The file is the user's own text, but it is a file on disk
				// that anything may have written, so it reaches the terminal
				// as the bytes it is and never as an instruction to it.
				_, _ = fmt.Fprint(env.Stdout, terminalSafe(in.Text))
				if !strings.HasSuffix(in.Text, "\n") {
					_, _ = fmt.Fprintln(env.Stdout)
				}
				return nil
			})
		},
	}
}

func newInstructionsSetCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Write an Account's standing instructions, read from standard input",
		Long: `Write an Account's standing instructions, read from standard input.

    owl account instructions set work < CLAUDE.md

Input that is blank takes the instructions away, leaving the Account as it was
before any were written.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := io.ReadAll(io.LimitReader(env.stdin(), maxInstructions+1))
			if err != nil {
				return fmt.Errorf("reading the instructions from standard input: %w", err)
			}
			if len(text) > maxInstructions {
				return fmt.Errorf("those instructions are longer than %d bytes; keep them shorter - "+
					"they are read into every run on the account", maxInstructions)
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				in, err := c.SetAccountInstructions(ctx, args[0], string(text))
				if err != nil {
					return err
				}
				return reportSaved(env, in)
			})
		},
	}
}

func newInstructionsEditCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <name>",
		Short: "Open an Account's standing instructions in your editor",
		Long: `Open an Account's standing instructions in $VISUAL, $EDITOR, or vi.

What the editor is left holding is what is saved. Leaving it blank takes the
instructions away; leaving it unchanged saves nothing.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			var before client.Instructions
			if err := withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				var err error
				before, err = c.AccountInstructions(ctx, name)
				return err
			}); err != nil {
				return err
			}
			if err := readable(before); err != nil {
				return err
			}
			// The editor runs outside the daemon call above: a person takes
			// as long as they take, and the deadline on a daemon request is
			// not a deadline on them.
			after, draft, err := editText(cmd.Context(), env, before)
			// The draft stays on disk until the save has gone through. What
			// the user typed exists nowhere else, and a save can be refused
			// for reasons they only find out about now - too long, the daemon
			// restarted while they were writing - so throwing it away on the
			// way to saying so would lose the very thing they came to write.
			// This is why git leaves COMMIT_EDITMSG behind.
			keep := func(cause error) error {
				return fmt.Errorf("%w\nwhat you wrote is still in %s", cause, draft)
			}
			if err != nil {
				return keep(err)
			}
			if after == before.Text {
				_, _ = fmt.Fprintf(env.Stdout, "%s is unchanged\n", before.File)
				return discard(draft)
			}
			if err := withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				in, err := c.SetAccountInstructions(ctx, name, after)
				if err != nil {
					return err
				}
				return reportSaved(env, in)
			}); err != nil {
				return keep(err)
			}
			return discard(draft)
		},
	}
}

// editText opens the Account's instructions in the user's editor and returns
// what they left behind, along with the path the draft is still at. The
// caller takes the draft away once the save has gone through, and leaves it
// where it is otherwise.
//
// The file is edited in a directory of Owl's own rather than in the Account's:
// an editor writes backups and swap files beside what it is editing, and the
// Account's directory is the tool's own state, not a scratch space.
func editText(ctx context.Context, env Env, in client.Instructions) (text, draft string, err error) {
	dir, err := os.MkdirTemp("", "owl-instructions-")
	if err != nil {
		return "", "", fmt.Errorf("making somewhere to edit the instructions: %w", err)
	}
	// Named as the tool names it, so the editor picks the highlighting and
	// the modeline a person expects for it.
	draft = filepath.Join(dir, in.File)
	if err := os.WriteFile(draft, []byte(in.Text), 0o600); err != nil {
		return "", draft, fmt.Errorf("writing the instructions out to edit: %w", err)
	}
	editor := firstSet("VISUAL", "EDITOR")
	if editor == "" {
		editor = fallbackEditor
	}
	// The editor owns the terminal for as long as it is up, so the interrupt
	// keys are its to answer rather than Owl's - without this, Ctrl-C in vim
	// ends Owl, the draft above is orphaned with nobody to say where it is,
	// and the editor carries on writing a file nothing will read back.
	defer holdInterrupts()()
	// Through a shell, because an editor is commonly set with its flags -
	// `code -w`, `emacs -nw` - and that is a command line, not a path. The
	// file is passed as a positional and quoted, so nothing in the path is
	// read as shell.
	run := exec.CommandContext(ctx, "/bin/sh", "-c", editor+` "$1"`, "sh", draft)
	run.Stdin, run.Stdout, run.Stderr = env.stdin(), env.Stdout, env.Stderr
	if err := run.Run(); err != nil {
		return "", draft, fmt.Errorf("running %s: %w", editor, err)
	}
	edited, err := os.ReadFile(draft)
	if err != nil {
		return "", draft, fmt.Errorf("reading back what %s saved: %w", editor, err)
	}
	return string(edited), draft, nil
}

// discard takes away a draft that has been saved, and says nothing about a
// failure to: the instructions are safely in the Account by then, and a
// leftover file in a temporary directory is not worth a message that would
// read as though the save had gone wrong.
func discard(draft string) error {
	_ = os.RemoveAll(filepath.Dir(draft))
	return nil
}

// firstSet is the value of the first of those variables that has one.
func firstSet(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// readable refuses an Account whose coding tool reads no standing
// instructions at all, by name, rather than showing an empty document or
// saving into nothing.
func readable(in client.Instructions) error {
	if in.File == "" {
		return fmt.Errorf("account %s is on the %s driver, whose tool reads no standing instructions",
			in.Account, in.Driver)
	}
	return nil
}

// reportSaved says what became of them, including that blank text took them
// away - a save that removes a file should not read as a save that wrote one.
func reportSaved(env Env, in client.Instructions) error {
	if strings.TrimSpace(in.Text) == "" {
		_, _ = fmt.Fprintf(env.Stdout, "cleared the standing instructions for account %s; %s is gone\n",
			in.Account, in.Path)
		return nil
	}
	_, _ = fmt.Fprintf(env.Stdout, "saved the standing instructions for account %s in %s\n", in.Account, in.Path)
	_, _ = fmt.Fprintln(env.Stdout, "every run on that account reads them, whichever project the run is for")
	return nil
}
