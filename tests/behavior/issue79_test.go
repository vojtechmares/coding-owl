package behavior_test

// Behavior tests for issue #79. TestS<n> maps to scenario S<n> in
// tests/behavior/issue-79.md. They read the repository the way a developer or
// CI does, and S4 builds the app the way make desktop does.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// frontendDir is where the desktop app's frontend lives.
func frontendDir() string { return filepath.Join(repoDir, "cmd", "owl-desktop", "frontend") }

func TestS1TheWailsConfigurationInvokesPnpmAndNothingElse(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoDir, "cmd", "owl-desktop", "wails.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("wails.json: %v", err)
	}

	want := map[string]string{
		"frontend:install":     "pnpm install --frozen-lockfile",
		"frontend:build":       "pnpm build",
		"frontend:dev:watcher": "pnpm dev",
	}
	for key, command := range want {
		if got, _ := cfg[key].(string); got != command {
			t.Errorf("wails.json %s = %q, want %q", key, got, command)
		}
	}
}

func TestS2TheFrontendPinsPnpmAndCarriesItsLockfile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(frontendDir(), "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("package.json: %v", err)
	}

	if !regexp.MustCompile(`^pnpm@\d+\.\d+\.\d+$`).MatchString(pkg.PackageManager) {
		t.Errorf("package.json packageManager = %q, want pnpm at an exact version", pkg.PackageManager)
	}
	if _, err := os.Stat(filepath.Join(frontendDir(), "pnpm-lock.yaml")); err != nil {
		t.Errorf("pnpm-lock.yaml: %v, want it present", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "ls-files", "--error-unmatch", "cmd/owl-desktop/frontend/pnpm-lock.yaml").CombinedOutput(); err != nil {
		t.Errorf("pnpm-lock.yaml is not committed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(frontendDir(), "package-lock.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("package-lock.json: stat err = %v, want it gone", err)
	}
}

func TestS3CIAndTheReleaseWorkflowUsePnpmAndItsCache(t *testing.T) {
	for _, name := range []string{"ci.yml", "release.yml"} {
		data, err := os.ReadFile(filepath.Join(repoDir, ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		pnpmSetup := strings.Index(text, "pnpm/action-setup")
		nodeSetup := strings.Index(text, "actions/setup-node")
		if pnpmSetup < 0 || nodeSetup < 0 || pnpmSetup > nodeSetup {
			t.Errorf("%s does not set pnpm up before Node (pnpm at %d, node at %d)", name, pnpmSetup, nodeSetup)
		}
		if !strings.Contains(text, "cache: pnpm") {
			t.Errorf("%s does not cache pnpm", name)
		}
		if !strings.Contains(text, "cache-dependency-path: cmd/owl-desktop/frontend/pnpm-lock.yaml") {
			t.Errorf("%s does not key the cache on the pnpm lockfile", name)
		}
		// Every npm left once the pnpms are taken out is npm itself.
		if strings.Contains(strings.ReplaceAll(text, "pnpm", ""), "npm") || strings.Contains(text, "package-lock") {
			t.Errorf("%s still names npm or its lockfile", name)
		}
	}
}

func TestS4TheAppBuildsWithPnpmAndNoNpmOnPATH(t *testing.T) {
	if os.Getenv("OWL_DESKTOP_BUILD") == "" {
		t.Skip("set OWL_DESKTOP_BUILD=1 to build the desktop app; it downloads the frontend's dependencies")
	}
	// An npm that fails when run, first on PATH: a build that reaches for npm
	// fails rather than quietly using it.
	shims := t.TempDir()
	shim := "#!/bin/sh\necho 'npm was run; the frontend is built with pnpm' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(shims, "npm"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shims, "npx"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("make", "desktop")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("make desktop with no usable npm: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(repoDir, "cmd", "owl-desktop", "build", "bin", "*.app"))
	if len(matches) == 0 {
		t.Errorf("make desktop left no app bundle under cmd/owl-desktop/build/bin")
	}
}

func TestS5NothingInTheRepositoryStillInvokesNpmForTheFrontend(t *testing.T) {
	invocation := regexp.MustCompile(`(^|[^p])npm (install|run|ci)\b|cache: npm|package-lock\.json`)
	var found []string
	err := filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "build", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repoDir, path)
		for i, ln := range strings.Split(string(data), "\n") {
			if invocation.MatchString(ln) {
				found = append(found, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(ln))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The spec sheet of this very issue describes what was replaced, and this
	// test names what it looks for.
	var others []string
	for _, f := range found {
		if !strings.HasPrefix(f, "tests/behavior/issue-79.md:") && !strings.HasPrefix(f, "tests/behavior/issue79_test.go:") {
			others = append(others, f)
		}
	}
	if len(others) > 0 {
		t.Errorf("npm is still invoked or named for the frontend:\n%s", strings.Join(others, "\n"))
	}

	for _, file := range []string{"Makefile", "README.md", filepath.Join("docs", "adr", "0009-wails-react-desktop.md")} {
		data, err := os.ReadFile(filepath.Join(repoDir, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "pnpm") {
			t.Errorf("%s does not name pnpm among the desktop app's prerequisites", file)
		}
	}
}
