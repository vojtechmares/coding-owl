package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/config"
)

func TestParseReadsBranchPrefix(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\nbranchPrefix: root/\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.BranchPrefix != "root/" {
		t.Errorf("BranchPrefix = %q, want %q", cfg.BranchPrefix, "root/")
	}
}

func TestParseRefusesMissingAPIVersion(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte("branchPrefix: nope/\n"))
	if err == nil {
		t.Fatal("Parse accepted a file with no apiVersion")
	}
	for _, want := range []string{"main:.coding-owl.yaml", "apiVersion"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestParseRefusesUnknownAPIVersion(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v99\nbranchPrefix: nope/\n"))
	if err == nil {
		t.Fatal("Parse accepted an unrecognised apiVersion")
	}
	for _, want := range []string{"main:.coding-owl.yaml", "apiVersion", "codingowl.dev/v99"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDefaultBranchPrefix(t *testing.T) {
	if got := config.Default().BranchPrefix; got != "owl/" {
		t.Errorf("Default().BranchPrefix = %q, want %q", got, "owl/")
	}
}

func TestParseFillsDefaultsForOmittedKeys(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.BranchPrefix != config.Default().BranchPrefix {
		t.Errorf("BranchPrefix = %q, want the default %q", cfg.BranchPrefix, config.Default().BranchPrefix)
	}
}

func TestParseRefusesABranchPrefixThatWouldReadAsAnOption(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml",
		[]byte("apiVersion: codingowl.dev/v1\nbranchPrefix: -f/\n"))

	if err == nil {
		t.Fatal("Parse with a dash-leading branchPrefix = nil, want an error")
	}
	if !strings.Contains(err.Error(), "-f/") || !strings.Contains(err.Error(), "dash") {
		t.Errorf("error %q does not say what is wrong with the prefix", err)
	}
}

func TestParseReadsAProjectsClausesAndSpendCap(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - run gofmt\nbudgetUSD: 2.5\n"))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.UnattendedClauses) != 1 || cfg.UnattendedClauses[0] != "run gofmt" {
		t.Errorf("clauses = %v, want the one the file carries", cfg.UnattendedClauses)
	}
	if cfg.BudgetUSD != 2.5 {
		t.Errorf("budget = %v, want 2.5", cfg.BudgetUSD)
	}
	if _, err := config.Parse("f", []byte("apiVersion: codingowl.dev/v1\nbudgetUSD: -1\n")); err == nil {
		t.Error("a negative spend cap was accepted")
	}
}

func TestParseReadsTheReviewAProjectAsksFor(t *testing.T) {
	// A review costs a second Agent invocation, so it is opt-in per Project
	// (ADR-0013).
	cfg, err := config.Parse("main:.coding-owl.yaml",
		[]byte("apiVersion: codingowl.dev/v1\nreview:\n  agent: true\n  timeout: 20m\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Review.Agent {
		t.Error("Review.Agent = false, want the review the file asks for")
	}
	if cfg.Review.Timeout != 20*time.Minute {
		t.Errorf("Review.Timeout = %v, want 20m", cfg.Review.Timeout)
	}
}

func TestParseLeavesAProjectThatAsksForNoReviewWithout(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Review.Agent {
		t.Error("Review.Agent = true for a file that asks for none")
	}
}

func TestParseRefusesAReviewTimeoutItCannotRead(t *testing.T) {
	for _, body := range []string{
		"apiVersion: codingowl.dev/v1\nreview:\n  agent: true\n  timeout: soon\n",
		"apiVersion: codingowl.dev/v1\nreview:\n  agent: true\n  timeout: -1m\n",
	} {
		_, err := config.Parse("main:.coding-owl.yaml", []byte(body))

		if err == nil {
			t.Errorf("Parse accepted %q", body)
		}
	}
}

func TestParseRefusesACheckCalledWhatAReviewIsCalled(t *testing.T) {
	// Verification reports a review beside the Project's own checks, and a
	// reader tells them apart by name (ADR-0013).
	_, err := config.Parse("main:.coding-owl.yaml",
		[]byte("apiVersion: codingowl.dev/v1\nchecks:\n  - name: "+config.ReviewName+"\n    run: \"true\"\n"))

	if err == nil {
		t.Fatal("Parse accepted a check called what a review is called")
	}
	if !strings.Contains(err.Error(), config.ReviewName) {
		t.Errorf("the error %q does not name it", err)
	}
}

func TestParseReadsPhases(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nphases:\n  plan:\n    model: haiku\n  execute:\n    effort: medium\n"))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := cfg.Phases[config.PhasePlan]; got.Model != "haiku" || got.Effort != "" {
		t.Errorf("plan phase = %+v, want haiku and nothing said about effort", got)
	}
	if got := cfg.Phases[config.PhaseExecute]; got.Effort != "medium" || got.Model != "" {
		t.Errorf("execute phase = %+v, want medium and nothing said about model", got)
	}
}

func TestParseRefusesAPhaseNobodyRuns(t *testing.T) {
	for name, body := range map[string]string{
		"unknown phase": "apiVersion: codingowl.dev/v1\nphases:\n  review:\n    model: opus\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Parse("f", []byte(body)); err == nil {
				t.Errorf("Parse(%q) = nil, want an error", body)
			}
		})
	}
}

func TestParseGlobalReadsPhasesAndRefusesAnUnknownAPIVersion(t *testing.T) {
	cfg, err := config.ParseGlobal("config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nphases:\n  execute:\n    effort: low\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if got := cfg.Phases[config.PhaseExecute].Effort; got != "low" {
		t.Errorf("execute effort = %q, want low", got)
	}
	if _, err := config.ParseGlobal("config.yaml", []byte("apiVersion: codingowl.dev/v99\n")); err == nil {
		t.Error("ParseGlobal accepted an apiVersion it does not recognise")
	}
}

func TestLoadGlobalReportsAMissingFileAsAbsentRatherThanBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	if _, found, err := config.LoadGlobal(path); err != nil || found {
		t.Fatalf("LoadGlobal on a missing file = %v, %v, want absent and no error", found, err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: codingowl.dev/v1\nphases:\n  plan:\n    model: sonnet\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, found, err := config.LoadGlobal(path)

	if err != nil || !found {
		t.Fatalf("LoadGlobal = %v, %v, want the file", found, err)
	}
	if got := cfg.Phases[config.PhasePlan].Model; got != "sonnet" {
		t.Errorf("plan model = %q, want sonnet", got)
	}
}

func TestParseReadsChecksAndSetup(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte(`apiVersion: codingowl.dev/v1
setup:
  - npm ci
checks:
  - name: build
    run: go build ./...
  - name: fmt
    run: gofmt -l .
    expect: empty_output
  - name: test
    run: go test ./...
    timeout: 10m
`))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Setup) != 1 || cfg.Setup[0] != "npm ci" {
		t.Errorf("setup = %v, want the command the file carries", cfg.Setup)
	}
	if len(cfg.Checks) != 3 {
		t.Fatalf("checks = %+v, want three", cfg.Checks)
	}
	if cfg.Checks[0] != (config.Check{Name: "build", Run: "go build ./..."}) {
		t.Errorf("build = %+v, want no expectation and no timeout of its own", cfg.Checks[0])
	}
	if cfg.Checks[1].Expect != config.ExpectEmptyOutput {
		t.Errorf("fmt expects %q, want %q", cfg.Checks[1].Expect, config.ExpectEmptyOutput)
	}
	if cfg.Checks[2].Timeout != 10*time.Minute {
		t.Errorf("test's timeout = %s, want 10m", cfg.Checks[2].Timeout)
	}
}

func TestParseRefusesACheckOwlCouldNotRunOrJudge(t *testing.T) {
	for name, body := range map[string]string{
		"no name":        "apiVersion: codingowl.dev/v1\nchecks:\n  - run: \"true\"\n",
		"no command":     "apiVersion: codingowl.dev/v1\nchecks:\n  - name: build\n",
		"unknown expect": "apiVersion: codingowl.dev/v1\nchecks:\n  - name: build\n    run: \"true\"\n    expect: no_warnings\n",
		"bad timeout":    "apiVersion: codingowl.dev/v1\nchecks:\n  - name: build\n    run: \"true\"\n    timeout: soon\n",
		"zero timeout":   "apiVersion: codingowl.dev/v1\nchecks:\n  - name: build\n    run: \"true\"\n    timeout: 0s\n",
		"same name":      "apiVersion: codingowl.dev/v1\nchecks:\n  - name: build\n    run: \"true\"\n  - name: build\n    run: \"false\"\n",
		"empty setup":    "apiVersion: codingowl.dev/v1\nsetup:\n  - \"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Parse("main:.coding-owl.yaml", []byte(body)); err == nil {
				t.Errorf("Parse accepted %s", name)
			}
		})
	}
}

func TestParseGlobalReadsTheGarbageCollectionBlock(t *testing.T) {
	got, err := config.ParseGlobal("config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\ngarbageCollection:\n  interval: 15m\n  reviewAfter: 48h\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if got.GarbageCollection.Interval != 15*time.Minute {
		t.Errorf("interval = %s, want 15m", got.GarbageCollection.Interval)
	}
	if got.GarbageCollection.ReviewAfter != 48*time.Hour {
		t.Errorf("reviewAfter = %s, want 48h", got.GarbageCollection.ReviewAfter)
	}
}

func TestParseGlobalLeavesGarbageCollectionToOwlWhenUnset(t *testing.T) {
	got, err := config.ParseGlobal("config.yaml", []byte("apiVersion: codingowl.dev/v1\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if got.GarbageCollection.Interval != 0 || got.GarbageCollection.ReviewAfter != 0 {
		t.Errorf("garbageCollection = %+v, want nothing set", got.GarbageCollection)
	}
}

func TestParseGlobalRefusesAGarbageCollectionDurationItCannotActOn(t *testing.T) {
	for _, body := range []string{
		"apiVersion: codingowl.dev/v1\ngarbageCollection:\n  interval: soon\n",
		"apiVersion: codingowl.dev/v1\ngarbageCollection:\n  interval: 0s\n",
		"apiVersion: codingowl.dev/v1\ngarbageCollection:\n  reviewAfter: -1h\n",
	} {
		_, err := config.ParseGlobal("config.yaml", []byte(body))

		if err == nil {
			t.Errorf("ParseGlobal(%q) = nil, want it refused", body)
			continue
		}
		if !strings.Contains(err.Error(), "garbageCollection") {
			t.Errorf("the error %q does not name the setting", err)
		}
	}
}

func TestParseReadsTheSkillsSection(t *testing.T) {
	got, err := config.Parse("main:.coding-owl.yaml", []byte(`apiVersion: codingowl.dev/v1
skills:
  - git: github.com/x/go-review
    ref: v1.4.0
  - git: github.com/me/house-style
    ref: main
    auto_update: true
`))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("skills = %+v, want two", got.Skills)
	}
	if got.Skills[0].Source != "github.com/x/go-review" || got.Skills[0].Ref != "v1.4.0" {
		t.Errorf("the first skill is %+v, want the source and ref it declares", got.Skills[0])
	}
	if got.Skills[0].AutoUpdate {
		t.Error("a skill that does not ask for it is set to update on its own")
	}
	if !got.Skills[1].AutoUpdate {
		t.Error("a skill that asks to update on its own does not")
	}
}

func TestParseRefusesASkillWithNoSource(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\nskills:\n  - ref: main\n"))

	if err == nil {
		t.Fatal("Parse of a skill with no source = nil, want it refused")
	}
	if !strings.Contains(err.Error(), "skills[0]") {
		t.Errorf("the error %q does not say which entry", err)
	}
}

func TestParseRefusesTwoSkillsOfOneName(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte(`apiVersion: codingowl.dev/v1
skills:
  - git: github.com/x/go-review
  - git: github.com/y/go-review
`))

	if err == nil {
		t.Fatal("Parse of two skills of one name = nil, want it refused")
	}
	if !strings.Contains(err.Error(), "go-review") {
		t.Errorf("the error %q does not name the skill", err)
	}
}

func TestParseRefusesASkillRefThatWouldBeAnOption(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml",
		[]byte("apiVersion: codingowl.dev/v1\nskills:\n  - git: github.com/x/go-review\n    ref: --upload-pack=evil\n"))

	if err == nil {
		t.Fatal("Parse of a dash-leading ref = nil, want it refused")
	}
}

func TestParseRefusesASkillThatWouldRewriteTheTerminal(t *testing.T) {
	// A manifest reaches the daemon from a base branch, which a merged pull
	// request writes, and both of these are printed back by owl skills list
	// and owl jobs show.
	for what, entry := range map[string]string{
		"source": "  - git: \"github.com/x/go-review\\u001b]0;pwned\\a\"\n",
		"ref":    "  - git: github.com/x/go-review\n    ref: \"main\\u001b]0;pwned\\a\"\n",
	} {
		_, err := config.Parse("main:.coding-owl.yaml",
			[]byte("apiVersion: codingowl.dev/v1\nskills:\n"+entry))

		if err == nil {
			t.Errorf("Parse of a %s holding an escape = nil, want it refused", what)
		}
	}
}

func TestParseGlobalReadsTheGraceWindow(t *testing.T) {
	cfg, err := config.ParseGlobal("config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\ngraceWindow: 90s\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if got, want := cfg.GraceWindow, 90*time.Second; got != want {
		t.Errorf("graceWindow = %s, want %s", got, want)
	}
	// Unset is not zero-with-a-meaning: it leaves the daemon's own default in
	// place, which the daemon decides.
	unset, err := config.ParseGlobal("config.yaml", []byte("apiVersion: codingowl.dev/v1\n"))
	if err != nil || unset.GraceWindow != 0 {
		t.Errorf("an unset graceWindow = %s, %v, want no window and no error", unset.GraceWindow, err)
	}
}

func TestParseGlobalRefusesAGraceWindowThatIsNotOne(t *testing.T) {
	for _, value := range []string{"soon", "15", "-5m", "0s"} {
		_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
			"apiVersion: codingowl.dev/v1\ngraceWindow: "+value+"\n"))

		if err == nil {
			t.Errorf("ParseGlobal accepted graceWindow: %q", value)
			continue
		}
		for _, want := range []string{"/somewhere/config.yaml", "graceWindow"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the error for %q does not name %q: %v", value, want, err)
			}
		}
	}
}
