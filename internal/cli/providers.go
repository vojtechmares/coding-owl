package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/chat"
	"github.com/vojtechmares/coding-owl/internal/client"
)

// noProviders is what owl providers list prints when none is configured.
const noProviders = "no model providers yet; owl providers add <name> --key-stdin configures one"

func newProvidersCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Manage the model providers the desktop chat speaks to",
		Long: `Manage the model providers the desktop chat speaks to.

The chat lives in the desktop app and is answered by the daemon, which holds
the keys: they go to the OS keychain, and the database keeps only a reference
to them (ADR-0019, ADR-0022). The key is typed here rather than in the app, so
the app never holds one.

Owl speaks to ` + strings.Join(chat.Providers, " and ") + `. Which models a
provider offers is Owl's to know for Anthropic and yours to say for
OpenRouter, which carries whatever your account has.`,
	}
	cmd.AddCommand(newProvidersAddCmd(env), newProvidersListCmd(env),
		newProvidersRemoveCmd(env), newProvidersSupportedCmd(env))
	return cmd
}

// providerAbout is what each provider is, in one line. Which providers there
// are belongs to internal/chat; how they are described to someone choosing
// between them is the CLI's, and the wording is the one the providers and
// providers add help already use, so the two do not drift into saying
// different things. TestProvidersSupportedNotesEveryProvider keeps a provider
// added to chat later from printing a blank line here.
var providerAbout = map[string]string{
	chat.Anthropic:  "The Anthropic API, spoken directly",
	chat.OpenRouter: "OpenRouter, which speaks the OpenAI shape for any model it offers",
}

// newProvidersSupportedCmd names the providers Owl drives, whether or not any
// of them is configured. It is the one providers command that does not ask the
// daemon: the user it is for has configured nothing and may not have a daemon
// running at all, which is why owl providers list cannot answer for them.
func newProvidersSupportedCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "supported",
		Short: "List the model providers Owl can be configured with",
		Long: `List the model providers Owl can be configured with.

These are the names "owl providers add <provider>" takes, and they are what
Owl was built with rather than what anybody configured: this answers before
there is anything to list, and without a daemon running.

Which models a provider offers is Owl's to know for Anthropic and yours to say
for OpenRouter, which carries whatever your account has.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, _ = fmt.Fprint(env.Stdout, "providers Owl drives:\n\n")
			w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "PROVIDER\tMODELS\tABOUT")
			for _, name := range chat.Providers {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", name, providerModels(name), providerAbout[name])
			}
			if err := w.Flush(); err != nil {
				return err
			}
			_, _ = fmt.Fprint(env.Stdout,
				"\nconfigure one with: owl providers add <provider> --key-stdin < key.txt\n")
			return nil
		},
	}
}

// providerModels says where a provider's models come from. It is derived from
// what chat holds rather than written out again, so that it cannot disagree
// with what owl providers add will accept: a provider with no models of Owl's
// own is one whose models the user has to name.
func providerModels(name string) string {
	if len(chat.DefaultModels[name]) > 0 {
		return "Owl's own; no --model needed"
	}
	return "yours; name them with --model"
}

func newProvidersAddCmd(env Env) *cobra.Command {
	var baseURL string
	var models []string
	var keyStdin bool
	cmd := &cobra.Command{
		Use:   "add <provider>",
		Short: "Configure a way to reach models",
		Long: `Configure a way to reach models.

The key is read from standard input rather than taken as a flag, so it is not
in the shell history or in what every process on the machine can see:

    owl providers add anthropic --key-stdin < key.txt

It is put in the credential store; the database records only that it is there.
Configuring a provider that is already configured replaces it, which is how a
key is rotated.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := providerKey(env, keyStdin)
			if err != nil {
				return err
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				p, err := c.AddProvider(ctx, args[0], key, baseURL, models)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "provider %s added, offering %s\n",
					p.Name, strings.Join(p.Models, ", "))
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&keyStdin, "key-stdin", false, "read the key from standard input")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "where to reach it (default: the provider's own)")
	cmd.Flags().StringArrayVar(&models, "model", nil,
		"a model it is to offer; repeat for more (required for openrouter)")
	return cmd
}

// providerKey is the key to configure, read from standard input. A key is not
// taken as a flag: an argument is in the shell history and in what every
// process on the machine can see, and this is a credential (ADR-0019).
func providerKey(env Env, fromStdin bool) (string, error) {
	if !fromStdin {
		return "", errors.New("a provider's key is read from standard input: pass --key-stdin")
	}
	key, err := io.ReadAll(io.LimitReader(env.stdin(), maxKey))
	if err != nil {
		return "", fmt.Errorf("reading the key from standard input: %w", err)
	}
	if strings.TrimSpace(string(key)) == "" {
		return "", errors.New("nothing was on standard input to read as a key")
	}
	return strings.TrimSpace(string(key)), nil
}

// maxKey bounds what is read as a key, so a mistaken `--key-stdin < some-huge-file`
// is refused rather than held in memory.
const maxKey = 64 << 10

func newProvidersListCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the configured model providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				providers, err := c.ListProviders(ctx)
				if err != nil {
					return err
				}
				if len(providers) == 0 {
					_, _ = fmt.Fprintln(env.Stdout, noProviders)
					return nil
				}
				w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "PROVIDER\tMODELS\tREACHED AT")
				for _, p := range providers {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
						p.Name, strings.Join(p.Models, ", "), orNone(p.BaseURL))
				}
				return w.Flush()
			})
		},
	}
	return cmd
}

func newProvidersRemoveCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <provider>",
		Short: "Stop speaking to a provider, and take its key away",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				if err := c.RemoveProvider(ctx, args[0]); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "provider %s removed, and its key with it\n", args[0])
				return nil
			})
		},
	}
	return cmd
}
