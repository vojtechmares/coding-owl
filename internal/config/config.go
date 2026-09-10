// Package config reads a Project's configuration file. The file is found by
// the discovery order in ADR-0014 and, for the in-repo forms, is read from the
// Project's base branch rather than from any working tree.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

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
	return cfg, nil
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
