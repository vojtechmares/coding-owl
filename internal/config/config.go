// Package config reads a Project's configuration file. The file is found by
// the discovery order in ADR-0014 and, for the in-repo forms, is read from the
// Project's base branch rather than from any working tree.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// APIVersion is the only apiVersion Owl recognises. A file carrying anything
// else is refused rather than guessed at (ADR-0014).
const APIVersion = "codingowl.dev/v1"

// Phase is what one phase of a Job runs at (ADR-0028). An empty field is one
// this file does not set, which leaves it to the level below.
type Phase struct {
	// Model is the model the Agent runs as.
	Model string
	// Effort is how hard it thinks.
	Effort string
}

// PhasePlan and PhaseExecute are the phases a Job passes through (ADR-0026).
// They are keys in a map rather than two fields, because a Job may grow more
// than two (ADR-0028).
const (
	PhasePlan    = "plan"
	PhaseExecute = "execute"
)

// Check is one Verification command (ADR-0030): a shell command with an
// optional expectation and a timeout. The command is written by the user and
// read from the Project's base branch, where the Agent being verified cannot
// reach it - which is what makes a shell command safe to run here and not in
// the chat (ADR-0022).
type Check struct {
	// Name identifies the check in a report.
	Name string
	// Run is the shell command.
	Run string
	// Expect is what passing means beyond exiting zero. Empty is exit zero
	// alone; ExpectEmptyOutput additionally requires an empty stdout.
	Expect string
	// Timeout bounds the check. Zero means the default.
	Timeout time.Duration
}

// ExpectEmptyOutput is the one expectation beyond exit zero. The vocabulary is
// deliberately tiny (ADR-0030).
const ExpectEmptyOutput = "empty_output"

// Config is a Project's effective configuration.
type Config struct {
	// BranchPrefix is prepended to the branch of every Job in the Project.
	BranchPrefix string
	// UnattendedClauses are the Project's own additions to Owl's standing
	// unattended contract (ADR-0017).
	UnattendedClauses []string
	// BudgetUSD caps what one Run of this Project may spend, when it is above
	// zero.
	BudgetUSD float64
	// Phases is what each phase of a Job runs at, keyed by phase name. A
	// phase or a field the file does not set is absent (ADR-0028).
	Phases map[string]Phase
	// Setup are shell commands run in a Job's worktree before an Agent starts,
	// for the untracked things a fresh worktree does not have (ADR-0007).
	Setup []string
	// Checks are what Verification runs after an execution Run (ADR-0013).
	Checks []Check
}

// Global is the daemon's own configuration, read from
// `<config home>/config.yaml` (ADR-0014). It sets the same per-phase model and
// effort a Project can, one level further out.
type Global struct {
	// Phases is what each phase runs at unless a Project or a Job says
	// otherwise.
	Phases map[string]Phase
}

// defaultBranchPrefix is what a Job's branch is prefixed with when no
// configuration file sets branchPrefix.
const defaultBranchPrefix = "owl/"

// Default is the configuration a Project has when no file is found anywhere in
// the discovery order.
func Default() Config {
	return Config{BranchPrefix: defaultBranchPrefix}
}

// file is the on-disk shape of a configuration file.
type file struct {
	APIVersion        string           `yaml:"apiVersion"`
	BranchPrefix      string           `yaml:"branchPrefix"`
	UnattendedClauses []string         `yaml:"unattendedClauses"`
	BudgetUSD         float64          `yaml:"budgetUSD"`
	Phases            map[string]phase `yaml:"phases"`
	Setup             []string         `yaml:"setup"`
	Checks            []check          `yaml:"checks"`
}

// check is the on-disk shape of one entry under `checks`.
type check struct {
	Name    string `yaml:"name"`
	Run     string `yaml:"run"`
	Expect  string `yaml:"expect"`
	Timeout string `yaml:"timeout"`
}

// phase is the on-disk shape of one entry under `phases`.
type phase struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

// Parse reads a configuration file. source names the file in error messages -
// `main:.coding-owl.yaml` for an in-repo form, an absolute path otherwise.
func Parse(source string, data []byte) (Config, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Config{}, fmt.Errorf("%s: %w", source, err)
	}
	if err := checkAPIVersion(source, f.APIVersion); err != nil {
		return Config{}, err
	}
	cfg := Default()
	if f.BranchPrefix != "" {
		// git reads an argument beginning with a dash as an option, and a
		// branch name is an argument to several of its commands.
		if strings.HasPrefix(f.BranchPrefix, "-") {
			return Config{}, fmt.Errorf("%s: branchPrefix %q may not start with a dash", source, f.BranchPrefix)
		}
		cfg.BranchPrefix = f.BranchPrefix
	}
	cfg.UnattendedClauses = f.UnattendedClauses
	if f.BudgetUSD < 0 {
		return Config{}, fmt.Errorf("%s: budgetUSD is %v; a spend cap cannot be negative", source, f.BudgetUSD)
	}
	cfg.BudgetUSD = f.BudgetUSD
	phases, err := parsePhases(source, f.Phases)
	if err != nil {
		return Config{}, err
	}
	cfg.Phases = phases
	checks, err := parseChecks(source, f.Checks)
	if err != nil {
		return Config{}, err
	}
	cfg.Checks = checks
	for i, cmd := range f.Setup {
		if strings.TrimSpace(cmd) == "" {
			return Config{}, fmt.Errorf("%s: setup command %d is empty", source, i+1)
		}
	}
	cfg.Setup = f.Setup
	return cfg, nil
}

// parseChecks reads the checks, refusing one Owl could not run or judge rather
// than discovering it after an Agent has spent a night.
func parseChecks(source string, checks []check) ([]Check, error) {
	if len(checks) == 0 {
		return nil, nil
	}
	out := make([]Check, 0, len(checks))
	seen := map[string]bool{}
	for i, c := range checks {
		name := strings.TrimSpace(c.Name)
		where := fmt.Sprintf("checks[%d]", i)
		if name != "" {
			where = "check " + strconv.Quote(name)
		}
		switch {
		case name == "":
			return nil, fmt.Errorf("%s: %s has no name", source, where)
		case seen[name]:
			return nil, fmt.Errorf("%s: %s is named twice; a report needs one name per check", source, where)
		case strings.TrimSpace(c.Run) == "":
			return nil, fmt.Errorf("%s: %s has no run command", source, where)
		case c.Expect != "" && c.Expect != ExpectEmptyOutput:
			return nil, fmt.Errorf("%s: %s expects %q, which Owl does not know; the only expectation is %q",
				source, where, c.Expect, ExpectEmptyOutput)
		}
		seen[name] = true
		parsed := Check{Name: name, Run: c.Run, Expect: c.Expect}
		if c.Timeout != "" {
			d, err := time.ParseDuration(c.Timeout)
			if err != nil {
				return nil, fmt.Errorf("%s: %s has an unreadable timeout %q: %w", source, where, c.Timeout, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("%s: %s has a timeout of %s; a check needs time to run", source, where, d)
			}
			parsed.Timeout = d
		}
		out = append(out, parsed)
	}
	return out, nil
}

// ParseGlobal reads the daemon's own configuration file, which sets what the
// phases of every Job run at unless a Project or a Job says otherwise.
func ParseGlobal(source string, data []byte) (Global, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Global{}, fmt.Errorf("%s: %w", source, err)
	}
	if err := checkAPIVersion(source, f.APIVersion); err != nil {
		return Global{}, err
	}
	phases, err := parsePhases(source, f.Phases)
	if err != nil {
		return Global{}, err
	}
	return Global{Phases: phases}, nil
}

// parsePhases reads the phases map, refusing a phase nobody runs and a value
// that could not be passed to a tool.
func parsePhases(source string, phases map[string]phase) (map[string]Phase, error) {
	if len(phases) == 0 {
		return nil, nil
	}
	out := make(map[string]Phase, len(phases))
	for name, p := range phases {
		if name != PhasePlan && name != PhaseExecute {
			return nil, fmt.Errorf("%s: phases has no %q; a job is planned then executed", source, name)
		}
		out[name] = Phase{Model: p.Model, Effort: p.Effort}
	}
	return out, nil
}

// checkAPIVersion refuses a file whose apiVersion Owl does not recognise
// rather than guessing at it (ADR-0014).
func checkAPIVersion(source, version string) error {
	if version == "" {
		return fmt.Errorf("%s: apiVersion is missing; expected %q", source, APIVersion)
	}
	if version != APIVersion {
		return fmt.Errorf("%s: apiVersion %q is not recognised; expected %q", source, version, APIVersion)
	}
	return nil
}

// LoadGlobal reads the daemon's own configuration file. found is false when
// there is no such file, which is not a failure: the defaults apply.
func LoadGlobal(path string) (cfg Global, found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Global{}, false, nil
	}
	if err != nil {
		return Global{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	cfg, err = ParseGlobal(path, data)
	if err != nil {
		return Global{}, false, err
	}
	return cfg, true, nil
}
