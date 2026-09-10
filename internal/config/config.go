// Package config reads a Project's configuration file. The file is found by
// the discovery order in ADR-0014 and, for the in-repo forms, is read from the
// Project's base branch rather than from any working tree.
package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// APIVersion is the only apiVersion Owl recognises. A file carrying anything
// else is refused rather than guessed at (ADR-0014).
const APIVersion = "codingowl.dev/v1"

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
	APIVersion        string   `yaml:"apiVersion"`
	BranchPrefix      string   `yaml:"branchPrefix"`
	UnattendedClauses []string `yaml:"unattendedClauses"`
	BudgetUSD         float64  `yaml:"budgetUSD"`
}

// Parse reads a configuration file. source names the file in error messages -
// `main:.coding-owl.yaml` for an in-repo form, an absolute path otherwise.
func Parse(source string, data []byte) (Config, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Config{}, fmt.Errorf("%s: %w", source, err)
	}
	if f.APIVersion == "" {
		return Config{}, fmt.Errorf("%s: apiVersion is missing; expected %q", source, APIVersion)
	}
	if f.APIVersion != APIVersion {
		return Config{}, fmt.Errorf("%s: apiVersion %q is not recognised; expected %q", source, f.APIVersion, APIVersion)
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
	return cfg, nil
}
