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
	"unicode"

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
// read from the Project's base branch before the Run it judges begins, which
// is what makes a shell command safe to run here and not in the chat
// (ADR-0022).
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
	// Review is the second pair of eyes a Project can ask for after its own
	// checks: a fresh Agent Session that reviews the work (ADR-0013).
	Review Review
	// Account names the Account every Job in this Project runs on (ADR-0023).
	// Empty is a Project that names none, which cannot run until it does.
	Account string
	// Skills are the Skills every Job in this Project gets, by source and ref
	// (ADR-0024). The manifest carries intent; the lockfile carries identity.
	Skills []Skill
}

// Review is what a Project asks of the reviewer. It costs a second Agent
// invocation per Run, so it is opt-in rather than the default (ADR-0013).
type Review struct {
	// Agent is whether a fresh Agent Session reviews the work.
	Agent bool
	// Timeout bounds the reviewer. Zero means the default.
	Timeout time.Duration
}

// Skill is one entry under `skills`: where it comes from, which ref it
// follows, and whether it may move on its own.
type Skill struct {
	// Source is the repository it comes from.
	Source string
	// Ref is the branch or tag it follows, empty for the source's default.
	Ref string
	// AutoUpdate is whether it is re-resolved at the start of every Run.
	// Pinning is the default, so this is opt-in per Skill (ADR-0024).
	AutoUpdate bool
}

// Global is the daemon's own configuration, read from
// `<config home>/config.yaml` (ADR-0014). It sets the same per-phase model and
// effort a Project can, one level further out.
type Global struct {
	// Phases is what each phase runs at unless a Project or a Job says
	// otherwise.
	Phases map[string]Phase
	// CredentialStore is where an Account's secret is kept: `keychain` or
	// `file` (ADR-0019). Empty leaves it to the platform, which is the
	// keychain where there is one.
	CredentialStore string
	// GarbageCollection is how often the task that keeps the worktrees honest
	// runs, and how long a Job may wait for a decision before it is reported
	// (ADR-0015). A zero field is one this file does not set.
	GarbageCollection GarbageCollection
	// GraceWindow is how long a frozen Run may stay frozen before Owl ends it
	// rather than leave a stopped process holding its sockets (ADR-0011). Zero
	// means whatever the daemon defaults to.
	GraceWindow time.Duration
}

// GarbageCollection is what the daemon's configuration says about the task
// that reclaims worktrees and reports unfinished work (ADR-0015).
type GarbageCollection struct {
	// Interval is how often it runs. Zero leaves it to Owl.
	Interval time.Duration
	// ReviewAfter is how long a Job may wait for a decision before it is
	// reported as unfinished work. Zero leaves it to Owl.
	ReviewAfter time.Duration
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
	Review            *review          `yaml:"review"`
	Skills            []skillEntry     `yaml:"skills"`
	Account           string           `yaml:"account"`
	CredentialStore   string           `yaml:"credentialStore"`
	GarbageCollection *garbage         `yaml:"garbageCollection"`
	GraceWindow       string           `yaml:"graceWindow"`
}

// review is the on-disk shape of the `review` block.
type review struct {
	Agent   bool   `yaml:"agent"`
	Timeout string `yaml:"timeout"`
}

// garbage is the on-disk shape of the `garbageCollection` block.
type garbage struct {
	Interval    string `yaml:"interval"`
	ReviewAfter string `yaml:"reviewAfter"`
}

// skillEntry is the on-disk shape of one entry under `skills`. `git` names the
// source, which is the ecosystem's own key for it.
type skillEntry struct {
	Git        string `yaml:"git"`
	Ref        string `yaml:"ref"`
	AutoUpdate bool   `yaml:"auto_update"`
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
	rev, err := parseReview(source, f.Review)
	if err != nil {
		return Config{}, err
	}
	cfg.Review = rev
	for i, cmd := range f.Setup {
		if strings.TrimSpace(cmd) == "" {
			return Config{}, fmt.Errorf("%s: setup command %d is empty", source, i+1)
		}
	}
	cfg.Setup = f.Setup
	// Which Account a Project's work draws on is a billing and policy matter,
	// so it is named rather than chosen (ADR-0023). Whether that Account
	// exists is the scheduler's question, not this file's.
	cfg.Account = strings.TrimSpace(f.Account)
	skills, err := parseSkills(source, f.Skills)
	if err != nil {
		return Config{}, err
	}
	cfg.Skills = skills
	return cfg, nil
}

// CheckText refuses a value that would print as something other than itself.
// A Skill's source and ref come out of a file a Project carries, which a
// merged pull request can change, and both are printed back to a terminal by
// `owl skills list` and `owl jobs show`.
func CheckText(what, value string) error {
	for _, r := range value {
		if r == '\t' || (unicode.IsPrint(r) && r != '\uFFFD') {
			continue
		}
		return fmt.Errorf("the %s %q holds a character Owl will not print", what, value)
	}
	return nil
}

// parseSkills reads the skills, refusing one Owl could not fetch or place
// rather than discovering it when a Run starts.
func parseSkills(source string, skills []skillEntry) ([]Skill, error) {
	if len(skills) == 0 {
		return nil, nil
	}
	out := make([]Skill, 0, len(skills))
	seen := map[string]bool{}
	for i, s := range skills {
		src := strings.TrimSpace(s.Git)
		where := fmt.Sprintf("skills[%d]", i)
		if src == "" {
			return nil, fmt.Errorf("%s: %s names no git source", source, where)
		}
		if err := CheckText("source", src); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", source, where, err)
		}
		// A Skill becomes a directory named after its source, and two of one
		// name would be one directory.
		name := skillName(src)
		if seen[name] {
			return nil, fmt.Errorf("%s: two skills are called %q; a worktree has one directory per name", source, name)
		}
		seen[name] = true
		ref := strings.TrimSpace(s.Ref)
		if strings.HasPrefix(ref, "-") {
			return nil, fmt.Errorf("%s: %s has the ref %q, which may not start with a dash", source, where, ref)
		}
		if err := CheckText("ref", ref); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", source, where, err)
		}
		out = append(out, Skill{Source: src, Ref: ref, AutoUpdate: s.AutoUpdate})
	}
	return out, nil
}

// skillName is the name a source yields, which is the last element of its path
// without a `.git` suffix. It is duplicated from internal/skill rather than
// imported, because that package reads this one's output and a cycle is worse
// than four lines.
func skillName(source string) string {
	s := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(source), "/"), ".git")
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// parseReview reads the `review` block. A Project that does not mention it
// asks for no review, which is what a Project that says nothing gets.
func parseReview(source string, r *review) (Review, error) {
	if r == nil {
		return Review{}, nil
	}
	out := Review{Agent: r.Agent}
	if r.Timeout == "" {
		return out, nil
	}
	d, err := time.ParseDuration(r.Timeout)
	if err != nil {
		return Review{}, fmt.Errorf("%s: review has an unreadable timeout %q: %w", source, r.Timeout, err)
	}
	if d <= 0 {
		return Review{}, fmt.Errorf("%s: review has a timeout of %s; a reviewer needs time to read", source, d)
	}
	out.Timeout = d
	return out, nil
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
	collection, err := parseGarbageCollection(source, f.GarbageCollection)
	if err != nil {
		return Global{}, err
	}
	grace, err := parseGraceWindow(source, f.GraceWindow)
	if err != nil {
		return Global{}, err
	}
	return Global{
		Phases:            phases,
		CredentialStore:   strings.TrimSpace(f.CredentialStore),
		GarbageCollection: collection,
		GraceWindow:       grace,
	}, nil
}

// parseGarbageCollection reads the `garbageCollection` block, refusing a
// duration Owl could not act on rather than quietly running on its default.
func parseGarbageCollection(source string, g *garbage) (GarbageCollection, error) {
	if g == nil {
		return GarbageCollection{}, nil
	}
	var out GarbageCollection
	for _, field := range []struct {
		name string
		text string
		into *time.Duration
	}{
		{name: "interval", text: g.Interval, into: &out.Interval},
		{name: "reviewAfter", text: g.ReviewAfter, into: &out.ReviewAfter},
	} {
		if strings.TrimSpace(field.text) == "" {
			continue
		}
		d, err := time.ParseDuration(field.text)
		if err != nil {
			return GarbageCollection{}, fmt.Errorf("%s: garbageCollection.%s is %q, which is not a length of time: %w",
				source, field.name, field.text, err)
		}
		if d <= 0 {
			return GarbageCollection{}, fmt.Errorf("%s: garbageCollection.%s is %s; it has to be some time at all",
				source, field.name, d)
		}
		*field.into = d
	}
	return out, nil
}

// parseGraceWindow reads how long a frozen Run may stay frozen. A window that
// is not a duration is refused rather than quietly defaulted: the setting
// exists to be believed.
func parseGraceWindow(source, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: graceWindow: %q is not a duration like 15m", source, value)
	}
	if d <= 0 {
		return 0, fmt.Errorf(
			"%s: graceWindow: %s would end a run the moment it was frozen; leave it out for the default",
			source, value)
	}
	return d, nil
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
