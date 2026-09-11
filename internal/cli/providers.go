package cli

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/chat"
	"github.com/vojtechmares/coding-owl/internal/client"
)

// noProviders is what owl providers list prints when none is configured.
const noProviders = "no model providers yet; owl providers add <name> --key <key> configures one"

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
	cmd.AddCommand(newProvidersAddCmd(env), newProvidersListCmd(env), newProvidersRemoveCmd(env))
	return cmd
}

func newProvidersAddCmd(env Env) *cobra.Command {
	var key, baseURL string
	var models []string
	cmd := &cobra.Command{
		Use:   "add <provider>",
		Short: "Configure a way to reach models",
		Long: `Configure a way to reach models.

The key is put in the credential store; the database records only that it is
there. Configuring a provider that is already configured replaces it, which is
how a key is rotated.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
	cmd.Flags().StringVar(&key, "key", "", "the credential to reach the provider with")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "where to reach it (default: the provider's own)")
	cmd.Flags().StringArrayVar(&models, "model", nil,
		"a model it is to offer; repeat for more (required for openrouter)")
	return cmd
}

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
