package config_test

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestParseReadsTheVerificationAProjectAsksFor(t *testing.T) {
	// The agent Verifier costs a second Agent invocation, so it is opt-in per
	// Project (ADR-0013).
	cfg, err := config.Parse("main:.coding-owl.yaml",
		[]byte("apiVersion: codingowl.dev/v1\nverification:\n  agent: true\n  timeout: 20m\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Verification.Agent {
		t.Error("Verification.Agent = false, want the agent verifier the file asks for")
	}
	if cfg.Verification.Timeout != 20*time.Minute {
		t.Errorf("Verification.Timeout = %v, want 20m", cfg.Verification.Timeout)
	}
}

func TestParseLeavesAProjectThatAsksForNoAgentVerifierWithout(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Verification.Agent {
		t.Error("Verification.Agent = true for a file that asks for none")
	}
}

func TestParseRefusesAVerificationTimeoutItCannotRead(t *testing.T) {
	for _, body := range []string{
		"apiVersion: codingowl.dev/v1\nverification:\n  agent: true\n  timeout: soon\n",
		"apiVersion: codingowl.dev/v1\nverification:\n  agent: true\n  timeout: -1m\n",
	} {
		_, err := config.Parse("main:.coding-owl.yaml", []byte(body))

		if err == nil {
			t.Errorf("Parse accepted %q", body)
		}
	}
}

func TestParseRefusesACheckCalledWhatTheAgentVerifierIsCalled(t *testing.T) {
	// Verification reports the agent Verifier beside the Project's own checks,
	// and a reader tells them apart by name (ADR-0013).
	_, err := config.Parse("main:.coding-owl.yaml",
		[]byte("apiVersion: codingowl.dev/v1\nchecks:\n  - name: "+config.AgentVerifierName+"\n    run: \"true\"\n"))

	if err == nil {
		t.Fatal("Parse accepted a check called what the agent verifier is called")
	}
	if !strings.Contains(err.Error(), config.AgentVerifierName) {
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

func TestParseGlobalReadsTheIdlePolicy(t *testing.T) {
	cfg, err := config.ParseGlobal("config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nidle:\n  after: 20m\n  requirePower: false\n  interval: 2s\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if got, want := cfg.Idle.After, 20*time.Minute; got != want {
		t.Errorf("idle.after = %s, want %s", got, want)
	}
	if got, want := cfg.Idle.Interval, 2*time.Second; got != want {
		t.Errorf("idle.interval = %s, want %s", got, want)
	}
	// Turning the power requirement off has to be tellable from not saying
	// anything about it, because the two mean opposite things.
	if cfg.Idle.RequirePower == nil || *cfg.Idle.RequirePower {
		t.Errorf("idle.requirePower = %v, want it read as off", cfg.Idle.RequirePower)
	}
	unset, err := config.ParseGlobal("config.yaml", []byte("apiVersion: codingowl.dev/v1\n"))
	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if unset.Idle.RequirePower != nil || unset.Idle.After != 0 || unset.Idle.Interval != 0 {
		t.Errorf("an unset idle policy = %+v, want nothing said", unset.Idle)
	}
}

func TestParseGlobalRefusesAnIdlePolicyThatIsNotOne(t *testing.T) {
	for _, body := range []string{
		"idle:\n  after: soon\n",
		"idle:\n  after: 10\n",
		"idle:\n  after: -1m\n",
		"idle:\n  after: 0s\n",
		"idle:\n  interval: never\n",
		"idle:\n  interval: 0s\n",
		"idle:\n  interval: 1ms\n",
	} {
		_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
			"apiVersion: codingowl.dev/v1\n"+body))

		if err == nil {
			t.Errorf("ParseGlobal accepted %q", body)
			continue
		}
		for _, want := range []string{"/somewhere/config.yaml", "idle."} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the error for %q does not name %q: %v", body, want, err)
			}
		}
	}
}

func TestParseGlobalReadsAnAccountsCeilings(t *testing.T) {
	cfg, err := config.ParseGlobal("config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\naccounts:\n  Work:\n    limits:\n"+
			"      fiveHourMax: 60\n      weeklyMax: 50%\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	// An Account is named case-insensitively, as it is everywhere else.
	got, ok := cfg.Ceiling("work")
	if !ok {
		t.Fatalf("no ceiling for work: %+v", cfg.Accounts)
	}
	if got.FiveHourMax != 60 || got.WeeklyMax != 50 {
		t.Errorf("ceiling = %+v, want 60 and 50", got)
	}
	// An Account nobody wrote down is held to nothing, which is not the same
	// as a ceiling of nothing.
	if _, ok := cfg.Ceiling("other"); ok {
		t.Error("an account nobody configured has a ceiling")
	}
}

func TestParseGlobalRefusesACeilingThatIsNotAShareOfAWindow(t *testing.T) {
	for _, value := range []string{"soon", "200", "-5", "0", "60%%"} {
		_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
			"apiVersion: codingowl.dev/v1\naccounts:\n  work:\n    limits:\n      fiveHourMax: "+
				strconv.Quote(value)+"\n"))

		if err == nil {
			t.Errorf("ParseGlobal accepted a ceiling of %q", value)
			continue
		}
		for _, want := range []string{"/somewhere/config.yaml", "fiveHourMax"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the error for %q does not name %q: %v", value, want, err)
			}
		}
	}
}

func TestParseGlobalTakesAnAccountWithNoLimitsBlock(t *testing.T) {
	cfg, err := config.ParseGlobal("config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\naccounts:\n  work: {}\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if _, ok := cfg.Ceiling("work"); ok {
		t.Error("an account with no limits block has a ceiling")
	}
}

func TestParseGlobalRefusesACeilingOwlDoesNotKeep(t *testing.T) {
	// The spelling ADR-0020 records, which this file does not use: a user who
	// copies it must be told rather than left with a ceiling nobody keeps.
	_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\naccounts:\n  work:\n    limits:\n      five_hour_max: 60%\n"))

	if err == nil {
		t.Fatal("ParseGlobal accepted a ceiling Owl does not keep")
	}
	for _, want := range []string{"/somewhere/config.yaml", "five_hour_max", "fiveHourMax"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not carry %q: %v", want, err)
		}
	}
}

func TestParseGlobalRefusesACeilingWithNothingAfterIt(t *testing.T) {
	// Somebody who wrote the key meant something by it; an empty value is not
	// the same as leaving it out.
	_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\naccounts:\n  work:\n    limits:\n      fiveHourMax:\n"))

	if err == nil {
		t.Fatal("ParseGlobal accepted a ceiling with nothing after it")
	}
	if !strings.Contains(err.Error(), "fiveHourMax") {
		t.Errorf("the error does not name the setting: %v", err)
	}
}

func TestParseGlobalRefusesOneAccountNamedTwice(t *testing.T) {
	// An Account is named case-insensitively, so these are one Account asking
	// to be held to two different ceilings.
	_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\naccounts:\n"+
			"  work:\n    limits:\n      fiveHourMax: 60\n"+
			"  Work:\n    limits:\n      fiveHourMax: 30\n"))

	if err == nil {
		t.Fatal("ParseGlobal accepted one account held to two ceilings")
	}
	for _, want := range []string{"work", "Work", "same account"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not carry %q: %v", want, err)
		}
	}
}

// Three caps, and the file is where two of them are written (ADR-0021).
func TestGlobalReadsHowManyRunsMayGoAtOnce(t *testing.T) {
	cfg, err := config.ParseGlobal("owl.yaml", []byte("apiVersion: codingowl.dev/v1\nmaxParallelRuns: 4\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if cfg.MaxParallelRuns != 4 {
		t.Errorf("maxParallelRuns = %d, want 4", cfg.MaxParallelRuns)
	}
	// Nothing runs in parallel until somebody asks, so a file that says
	// nothing says one.
	cfg, err = config.ParseGlobal("owl.yaml", []byte("apiVersion: codingowl.dev/v1\n"))
	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if cfg.MaxParallelRuns != 1 {
		t.Errorf("maxParallelRuns = %d with nothing set, want 1", cfg.MaxParallelRuns)
	}
}

// And a Project raises its own deliberately, once somebody knows its checks are
// hermetic (ADR-0021).
func TestAProjectSaysHowManyOfItsOwnRunsMayGoAtOnce(t *testing.T) {
	cfg, err := config.Parse(".coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\nmaxParallelRuns: 2\n"))

	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.MaxParallelRuns != 2 {
		t.Errorf("maxParallelRuns = %d, want 2", cfg.MaxParallelRuns)
	}
	if got := config.Default().MaxParallelRuns; got != 1 {
		t.Errorf("a project that says nothing runs %d at once, want 1", got)
	}
}

// A cap that is not one is refused by name: a decoder's own message says a line
// number and not the setting.
func TestACapThatIsNotOneIsRefusedByName(t *testing.T) {
	for _, bad := range []string{
		"maxParallelRuns: 0\n",
		"maxParallelRuns: -1\n",
		"maxParallelRuns: many\n",
		"maxParallelRuns: 1.5\n",
		"accounts:\n  work:\n    maxParallel: 0\n",
		"accounts:\n  work:\n    maxParallel: nope\n",
	} {
		_, err := config.ParseGlobal("owl.yaml", []byte("apiVersion: codingowl.dev/v1\n"+bad))

		if err == nil {
			t.Errorf("ParseGlobal accepted %q", bad)
			continue
		}
		if !strings.Contains(err.Error(), "maxParallel") {
			t.Errorf("the refusal for %q does not name the setting: %v", bad, err)
		}
	}
}

// refusedNaming checks a parse was refused with an error that names each of
// the given things, which for issue #54 is the unknown key and the file.
func refusedNaming(t *testing.T, err error, what string, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s was accepted, want it refused", what)
	}
	for _, name := range names {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error for %s does not name %q: %v", what, name, err)
		}
	}
}

func TestS1AProjectFileWithAnUnknownTopLevelKeyIsRefusedByName(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\nmaxParalellRuns: 2\n"))

	refusedNaming(t, err, "a project file with a misspelled key", "maxParalellRuns", "main:.coding-owl.yaml")
}

func TestS2AGlobalFileWithAProjectOnlyBlockIsRefusedByName(t *testing.T) {
	_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nchecks:\n  - name: tests\n    run: make test\n"))

	refusedNaming(t, err, "a daemon file with a checks block", "checks", "/somewhere/config.yaml")
}

func TestS3AProjectFileWithAGlobalOnlyKeyIsRefusedByName(t *testing.T) {
	_, err := config.Parse("main:.coding-owl.yaml", []byte("apiVersion: codingowl.dev/v1\ncredentialStore: file\n"))

	refusedNaming(t, err, "a project file with credentialStore", "credentialStore", "main:.coding-owl.yaml")
}

func TestS4AnUnknownKeyInsideABlockIsRefusedByName(t *testing.T) {
	_, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nidle:\n  afterr: 5m\n"))
	refusedNaming(t, err, "a daemon file with a misspelled idle setting", "afterr", "/somewhere/config.yaml")

	_, err = config.Parse("main:.coding-owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nchecks:\n  - name: tests\n    runn: make test\n"))
	refusedNaming(t, err, "a project file with a misspelled check setting", "runn", "main:.coding-owl.yaml")
}

// S4 of tests/behavior/issue-56.md: a Project may extend the allowlist its
// Agents run with, and an entry that names nothing is refused by name.
func TestS4AProjectFileMayExtendTheAllowlist(t *testing.T) {
	cfg, err := config.Parse("main:.coding-owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nallowedTools:\n  - \"Bash(make test:*)\"\n  - WebFetch\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := []string{"Bash(make test:*)", "WebFetch"}; strings.Join(cfg.AllowedTools, ",") != strings.Join(want, ",") {
		t.Errorf("allowedTools = %v, want %v in order", cfg.AllowedTools, want)
	}

	_, err = config.Parse("main:.coding-owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nallowedTools:\n  - WebFetch\n  - \"  \"\n"))

	refusedNaming(t, err, "a project file with an empty allowed tool", "allowedTools", "2", "main:.coding-owl.yaml")

	_, err = config.Parse("main:.coding-owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nallowedTools:\n  - \"Bash(make test:*),WebFetch\"\n"))

	refusedNaming(t, err, "a project file with a comma in an allowed tool", "allowedTools", "comma", "main:.coding-owl.yaml")
}

// S4 of tests/behavior/issue-57.md: the daemon's file may say where the tool
// is, as a path that means the same wherever the daemon was started from.
func TestS4TheDaemonsFileMayNameTheToolsPath(t *testing.T) {
	cfg, err := config.ParseGlobal("/somewhere/config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nclaudePath: /opt/tools/claude\n"))
	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if cfg.ClaudePath != "/opt/tools/claude" {
		t.Errorf("claudePath = %q, want the path the file named", cfg.ClaudePath)
	}

	_, err = config.ParseGlobal("/somewhere/config.yaml", []byte(
		"apiVersion: codingowl.dev/v1\nclaudePath: tools/claude\n"))

	refusedNaming(t, err, "a daemon file whose claudePath is relative", "claudePath", "tools/claude", "/somewhere/config.yaml")
}

// An Account with no cap of its own is held only by the other two: burn rate is
// already governed by the ceiling (ADR-0021).
func TestAnAccountWithNoCapOfItsOwnIsUnlimited(t *testing.T) {
	cfg, err := config.ParseGlobal("owl.yaml", []byte(
		"apiVersion: codingowl.dev/v1\naccounts:\n  work:\n    maxParallel: 2\n  other:\n    limits:\n      fiveHourMax: 60\n"))

	if err != nil {
		t.Fatalf("ParseGlobal: %v", err)
	}
	if got, _ := cfg.Ceiling("work"); got.MaxParallel != 2 {
		t.Errorf("work runs %d at once, want 2", got.MaxParallel)
	}
	if got, _ := cfg.Ceiling("other"); got.MaxParallel != 0 {
		t.Errorf("an account that says nothing runs %d at once, want as many as the others allow", got.MaxParallel)
	}
}
