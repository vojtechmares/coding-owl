package behavior_test

// Behavior tests for issue #120. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-120.md. They drive the built owl binary from the
// outside; only S5 and S6 start a daemon, and they start one to show that what
// is configured makes no difference to what is supported.

import (
	"slices"
	"strings"
	"testing"
)

// supported is what `owl providers supported` printed, which every scenario
// here reads. It takes no daemon on purpose: a first-time user has configured
// nothing and may not have one running, and that is the whole point of the
// command.
func supported(t *testing.T, l *layout) string {
	t.Helper()
	res := runOwl(t, l, "providers", "supported")
	if res.code != 0 {
		t.Fatalf("owl providers supported exited %d\nstdout:\n%s\nstderr:\n%s",
			res.code, res.stdout, res.stderr)
	}
	return res.stdout
}

// supportedNames are the provider names the command printed, read out of the
// first column of its table: the rows between the PROVIDER header and the
// blank line that ends the table, and nothing of the prose around it. The
// scenarios read them back rather than naming them, so that S5 ties the
// listing to what `owl providers add` accepts rather than to a second list
// written in the test.
func supportedNames(t *testing.T, out string) []string {
	t.Helper()
	var names []string
	inTable := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "PROVIDER" {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if len(fields) == 0 {
			break
		}
		names = append(names, fields[0])
	}
	if !inTable {
		t.Fatalf("owl providers supported printed no PROVIDER table:\n%s", out)
	}
	return names
}

// providerLine is the row the command printed for a provider, which is where
// the note about how its models are decided lives.
func providerLine(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == name {
			return line
		}
	}
	t.Fatalf("owl providers supported printed no line for %q:\n%s", name, out)
	return ""
}

func TestS1ProvidersSupportedNamesThemWithNothingConfigured(t *testing.T) {
	l := newLayout(t)

	res := runOwl(t, l, "providers", "supported")

	if res.code != 0 {
		t.Fatalf("owl providers supported exited %d with no daemon up\nstdout:\n%s\nstderr:\n%s",
			res.code, res.stdout, res.stderr)
	}
	out := res.stdout
	// Named as a row of the listing, rather than mentioned somewhere in prose:
	// the prose is what the user already had, and is what this replaces.
	listed := supportedNames(t, out)
	for _, want := range []string{"anthropic", "openrouter"} {
		if !slices.Contains(listed, want) {
			t.Errorf("owl providers supported does not list %q; it listed %v:\n%s", want, listed, out)
		}
	}
	// It answered from what Owl was built with, so nothing of a daemon it
	// could not reach belongs in what it wrote, on either stream.
	both := strings.ToLower(out + res.stderr)
	for _, unwanted := range []string{"socket", "connection refused", "is the daemon running", "no such file"} {
		if strings.Contains(both, unwanted) {
			t.Errorf("owl providers supported mentions %q, so it went looking for a daemon:\n%s%s",
				unwanted, out, res.stderr)
		}
	}
	// The gap this fills: the command that lists what is configured cannot
	// answer the question at all here.
	configured := runOwl(t, l, "providers", "list")
	if configured.code == 0 && strings.Contains(configured.stdout, "openrouter") {
		t.Errorf("owl providers list already names the supported providers, so nothing was missing:\n%s",
			configured.stdout)
	}
}

func TestS2ProvidersSupportedSaysHowEachOnesModelsAreDecided(t *testing.T) {
	l := newLayout(t)

	out := supported(t, l)

	anthropic := providerLine(t, out, "anthropic")
	// Said outright, not merely left unsaid: a line that never mentions the
	// flag leaves the user no better off than before.
	if !strings.Contains(anthropic, "no --model") {
		t.Errorf("the anthropic line does not say --model is not needed:\n%s", anthropic)
	}
	if !strings.Contains(anthropic, "Owl") {
		t.Errorf("the anthropic line does not say its models are Owl's own:\n%s", anthropic)
	}
	openrouter := providerLine(t, out, "openrouter")
	if !strings.Contains(openrouter, "--model") {
		t.Errorf("the openrouter line does not name --model, which it needs:\n%s", openrouter)
	}
	if !strings.Contains(openrouter, "your") && !strings.Contains(openrouter, "account") {
		t.Errorf("the openrouter line does not say the models are the account's own:\n%s", openrouter)
	}
}

func TestS3ProvidersSupportedSaysHowToConfigureOne(t *testing.T) {
	l := newLayout(t)

	out := supported(t, l)

	for _, want := range []string{"owl providers add", "--key-stdin"} {
		if !strings.Contains(out, want) {
			t.Errorf("owl providers supported does not say %q, so it leads nowhere:\n%s", want, out)
		}
	}
}

func TestS4ProvidersSupportedIsDiscoverableAndTakesNoArguments(t *testing.T) {
	l := newLayout(t)

	help := mustOwl(t, l, "providers", "--help").stdout

	// In the list of commands, beside the other three - not somewhere in the
	// prose, which is the discoverable-by-accident state this command exists
	// to end.
	_, commands, found := strings.Cut(help, "Available Commands:")
	if !found {
		t.Fatalf("owl providers --help lists no commands at all:\n%s", help)
	}
	for _, want := range []string{"add", "list", "remove", "supported"} {
		if !strings.Contains(commands, "\n  "+want+" ") {
			t.Errorf("owl providers --help does not list %q among its commands:\n%s", want, commands)
		}
	}
	res := runOwl(t, l, "providers", "supported", "nonsense")
	if res.code == 0 {
		t.Fatalf("owl providers supported took an argument it has no use for:\n%s", res.stdout)
	}
}

func TestS5ProvidersSupportedListsWhatProvidersAddAccepts(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)

	names := supportedNames(t, supported(t, l))

	if len(names) == 0 {
		t.Fatal("owl providers supported printed no provider names at all")
	}
	const notDriven = "is not a provider Owl drives"
	for _, name := range names {
		// A refusal for a missing --model is fine and is S2's point; a refusal
		// that says Owl does not drive it at all means the listing and what
		// `add` accepts have come apart. Any other refusal is checked too, so
		// that a daemon which answered nothing useful cannot pass this by
		// failing every add for some third reason.
		res := runOwlStdin(t, l, "sk-test", "providers", "add", name, "--key-stdin")
		if strings.Contains(res.stderr, notDriven) {
			t.Errorf("owl providers supported lists %q, which owl providers add refuses:\n%s", name, res.stderr)
			continue
		}
		if res.code != 0 && !strings.Contains(res.stderr, "--model") {
			t.Errorf("owl providers add %s failed for something other than the models it wants:\n%s",
				name, res.stderr)
		}
	}
	// And the other way round: a name it did not print is not one Owl drives.
	res := runOwlStdin(t, l, "sk-test", "providers", "add", "openai", "--key-stdin")
	if res.code == 0 {
		t.Fatal("owl providers add accepted openai, which owl providers supported does not list")
	}
	if !strings.Contains(res.stderr, notDriven) {
		t.Errorf("stderr does not say openai is not a provider Owl drives:\n%s", res.stderr)
	}
}

func TestS6ProvidersSupportedDoesNotChangeWithWhatIsConfigured(t *testing.T) {
	bare := newLayout(t)
	before := supported(t, bare)

	l := newLayout(t)
	daemonUp(t, l)
	addProvider(t, l, "anthropic", "sk-ant-test", "")

	// The one that is configured is there, and so is the one that is not.
	listed := mustOwl(t, l, "providers", "list").stdout
	if !strings.Contains(listed, "anthropic") {
		t.Fatalf("the scenario's own setup did not configure anthropic:\n%s", listed)
	}
	if strings.Contains(listed, "openrouter") {
		t.Fatalf("the scenario wants openrouter left unconfigured:\n%s", listed)
	}
	after := supported(t, l)
	names := supportedNames(t, after)
	for _, want := range []string{"anthropic", "openrouter"} {
		if !slices.Contains(names, want) {
			t.Errorf("owl providers supported does not list %q with a provider configured; it listed %v:\n%s",
				want, names, after)
		}
	}
	if after != before {
		t.Errorf("what owl providers supported prints changed with what is configured:\nwith nothing:\n%s\nwith anthropic:\n%s", before, after)
	}
}
