package behavior_test

// Behavior tests for issue #134. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-134.md.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configHomeDir134 is a Project's configuration directory under the config
// home - ADR-0014 form 4 - which is the only place these scenarios put a
// near-miss file. `projectConfig` writes `config.yaml` there and nothing else,
// and every scenario here is about some other name.
func configHomeDir134(l *layout, name string) string {
	return filepath.Join(l.config, "coding-owl", name)
}

// strayConfig134 writes file into the Project's configuration directory.
func strayConfig134(t *testing.T, l *layout, name, file, content string) string {
	t.Helper()
	path := filepath.Join(configHomeDir134(l, name), file)
	writeFile(t, path, content)
	return path
}

// unconfiguredProject134 registers a Project whose repository carries no Owl
// file on its base branch, so discovery falls through to form 4.
func unconfiguredProject134(t *testing.T, l *layout) *repo {
	t.Helper()
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	return r
}

func TestS1NearMissConfigIsNamed(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	unconfiguredProject134(t, l)
	strayConfig134(t, l, "api", ".coding-owl.yaml", owlConfig("stray/"))

	res := mustOwl(t, l, "project", "show", "api")

	got := line(t, res.stdout, "config")
	if !strings.HasPrefix(got, "(none)") {
		t.Errorf("config line = %q, want it to still begin (none): nothing was loaded", got)
	}
	for _, want := range []string{".coding-owl.yaml", configHomeDir134(l, "api"), "config.yaml"} {
		if !strings.Contains(got, want) {
			t.Errorf("config line = %q, does not name %q", got, want)
		}
	}
}

func TestS2NearMissConfigIsReportedNotLoaded(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	unconfiguredProject134(t, l)
	body := "apiVersion: codingowl.dev/v1\nbranchPrefix: stray/\naccount: nobody\n"
	stray := strayConfig134(t, l, "api", ".coding-owl.yaml", body)

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "branch prefix", "owl/")
	wantLine(t, show, "account", "(none)")
	got, err := os.ReadFile(stray)
	if err != nil {
		t.Fatalf("the near-miss file is gone: %v", err)
	}
	if string(got) != body {
		t.Errorf("near-miss file = %q, want it left byte-identical at %q", got, body)
	}
	if _, err := os.Stat(filepath.Join(configHomeDir134(l, "api"), "config.yaml")); err == nil {
		t.Error("a config.yaml appeared beside the near-miss file; the report must not put one there")
	}
}

func TestS3NearMissYmlCounts(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	unconfiguredProject134(t, l)
	strayConfig134(t, l, "api", "coding-owl.yml", owlConfig("stray/"))

	got := line(t, mustOwl(t, l, "project", "show", "api").stdout, "config")

	for _, want := range []string{"coding-owl.yml", "config.yaml"} {
		if !strings.Contains(got, want) {
			t.Errorf("config line = %q, does not name %q", got, want)
		}
	}
}

func TestS4ConfigThatWasFoundSaysNothing(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	unconfiguredProject134(t, l)
	found := strayConfig134(t, l, "api", "config.yaml", owlConfig("fallback/"))
	strayConfig134(t, l, "api", ".coding-owl.yaml", owlConfig("stray/"))

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "config", found)
	if got := line(t, show, "config"); strings.Contains(got, ".coding-owl.yaml") {
		t.Errorf("config line = %q, names a near miss although a configuration was found", got)
	}
}

func TestS5InRepoConfigSaysNothing(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", owlConfig("root/"), "add config")
	addProject(t, l, r)
	strayConfig134(t, l, "api", ".coding-owl.yaml", owlConfig("stray/"))

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "config", "main:.coding-owl.yaml")
}

func TestS6FilesThatAreNotNearMissesAreNotReported(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	unconfiguredProject134(t, l)
	strayConfig134(t, l, "api", ".coding-owl.lock.yaml", "apiVersion: codingowl.dev/v1\n")
	strayConfig134(t, l, "api", "notes.txt", "remember to configure this\n")

	show := mustOwl(t, l, "project", "show", "api").stdout

	wantLine(t, show, "config", "(none)")
}

func TestS7OneNearMissIsNamedWhenSeveralAreThere(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	unconfiguredProject134(t, l)
	strayConfig134(t, l, "api", "alpha.yaml", owlConfig("alpha/"))
	strayConfig134(t, l, "api", "coding-owl.yaml", owlConfig("stray/"))

	got := line(t, mustOwl(t, l, "project", "show", "api").stdout, "config")

	if !strings.Contains(got, "coding-owl.yaml") {
		t.Errorf("config line = %q, does not name coding-owl.yaml, the likelier near miss", got)
	}
	if strings.Contains(got, "alpha.yaml") {
		t.Errorf("config line = %q, names alpha.yaml, which merely sorts first", got)
	}
}

func TestS8HostileNearMissNameIsPrintedAsText(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	unconfiguredProject134(t, l)
	strayConfig134(t, l, "api", "\x1b]0;pwned\aowl.yaml", owlConfig("stray/"))

	show := mustOwl(t, l, "project", "show", "api").stdout

	if strings.ContainsRune(show, 0x1b) {
		t.Errorf("owl project show passed a raw escape byte to the terminal:\n%q", show)
	}
	if got := line(t, show, "config"); !strings.Contains(got, `\x1b`) {
		t.Errorf("config line = %q, does not show the escape as the bytes it is", got)
	}
}
