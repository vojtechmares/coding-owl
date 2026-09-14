// Package config reads a Project's configuration file. The file is found by
// the discovery order in ADR-0014 and, for the in-repo forms, is read from the
// Project's base branch rather than from any working tree.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
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

// AgentVerifierName is what the Verification an Agent performs is called
// where Verification reports what it said, beside the Project's own checks
// (ADR-0013). No check may be called it.
const AgentVerifierName = "agent verifier"

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
	// Verification is what a Project asks of Verification beyond its own
	// checks: the fresh Agent Session that reviews the work (ADR-0013).
	Verification Verification
	// Account names the Account every Job in this Project runs on (ADR-0023).
	// Empty is a Project that names none, which cannot run until it does.
	Account string
	// Skills are the Skills every Job in this Project gets, by source and ref
	// (ADR-0024). The manifest carries intent; the lockfile carries identity.
	Skills []Skill
	// AllowedTools are the tool rules this Project grants its Agents on top
	// of the Account's own allowlist, in the tool's own syntax (ADR-0035).
	AllowedTools []string
	// MaxParallelRuns is how many Runs of this Project may go at once. One
	// unless the file says otherwise: two worktrees of one repository will
	// each happily bind the same port and write the same test database, so
	// raising it is a Project-specific decision by somebody who knows those
	// checks are hermetic (ADR-0021).
	MaxParallelRuns int
}

// Verification is what a Project asks of Verification beyond its own checks.
// The agent Verifier costs a second Agent invocation per Run, so it is opt-in
// rather than the default (ADR-0013).
type Verification struct {
	// Agent is whether a fresh Agent Session reviews the work.
	Agent bool
	// Timeout bounds that Session. Zero means the default.
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
	// Idle is when Owl may work: what counts as a machine nobody is at, and
	// how often it is looked at (ADR-0011).
	Idle Idle
	// Accounts is what each named Account is held to, keyed by its name in
	// lower case: Account names are case-insensitive (ADR-0019).
	Accounts map[string]Limits
	// MaxParallelRuns is how many Runs Owl carries out at once, across every
	// Project. One unless the file says otherwise: nothing runs in parallel
	// until somebody asks (ADR-0021).
	MaxParallelRuns int
}

// Limits is the ceiling an Account's utilization is held to, per window
// (ADR-0020). Utilization is account-wide, so a ceiling is what Owl leaves the
// user rather than what Owl may spend. A zero field is one nobody set.
type Limits struct {
	// MaxParallel is how many Runs may draw on this Account at once, zero for
	// as many as the other caps allow: burn rate is already governed by the
	// ceiling (ADR-0021).
	MaxParallel int
	// FiveHourMax is the most of the five-hour window Owl will work into, as
	// a percentage.
	FiveHourMax float64
	// WeeklyMax is the same for the seven-day window, compared against the
	// most utilized of them (ADR-0020).
	WeeklyMax float64
}

// Ceiling is the ceiling for that Account, and whether one was set at all.
func (g Global) Ceiling(account string) (Limits, bool) {
	l, ok := g.Accounts[strings.ToLower(strings.TrimSpace(account))]
	return l, ok
}

// Idle is what the daemon's configuration says about when Owl may work
// (ADR-0011). A field this file does not set is left to Owl.
type Idle struct {
	// After is how long the machine must have been without input. Zero leaves
	// it to Owl.
	After time.Duration
	// RequirePower is whether the machine must be on AC power. Nil leaves it
	// to Owl, which requires it: unattended work on a battery flattens it.
	RequirePower *bool
	// Interval is how often the machine is looked at. Zero leaves it to Owl.
	Interval time.Duration
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

// DefaultGlobal is the daemon's own configuration when it has no file: one Run
// at a time, and nothing else set (ADR-0021).
func DefaultGlobal() Global {
	return Global{MaxParallelRuns: 1}
}

// Default is the configuration a Project has when no file is found anywhere in
// the discovery order.
func Default() Config {
	return Config{BranchPrefix: defaultBranchPrefix, MaxParallelRuns: 1}
}

// file is the on-disk shape of a configuration file.
// projectFile is the on-disk shape of a Project's `.coding-owl.yaml`. Each
// file kind has a shape of its own and is decoded strictly against it, so
// that a key the shape does not have - a misspelling, or a key that belongs
// to the daemon's file - is refused by name rather than ignored.
type projectFile struct {
	APIVersion        string           `yaml:"apiVersion"`
	BranchPrefix      string           `yaml:"branchPrefix"`
	UnattendedClauses []string         `yaml:"unattendedClauses"`
	BudgetUSD         float64          `yaml:"budgetUSD"`
	Phases            map[string]phase `yaml:"phases"`
	Setup             []string         `yaml:"setup"`
	Checks            []check          `yaml:"checks"`
	Verification      *verification    `yaml:"verification"`
	Skills            []skillEntry     `yaml:"skills"`
	Account           string           `yaml:"account"`
	AllowedTools      []string         `yaml:"allowedTools"`
	// MaxParallelRuns is read as what was written rather than into an int, so
	// that a value nobody can read is refused by name: a decoder's own message
	// says the line number and not the setting (ADR-0021).
	MaxParallelRuns any `yaml:"maxParallelRuns"`
}

// globalFile is the on-disk shape of the daemon's own `config.yaml`, decoded
// strictly for the same reason projectFile is.
type globalFile struct {
	APIVersion        string                  `yaml:"apiVersion"`
	Phases            map[string]phase        `yaml:"phases"`
	CredentialStore   string                  `yaml:"credentialStore"`
	GarbageCollection *garbage                `yaml:"garbageCollection"`
	GraceWindow       string                  `yaml:"graceWindow"`
	Idle              *idlePolicy             `yaml:"idle"`
	MaxParallelRuns   any                     `yaml:"maxParallelRuns"`
	Accounts          map[string]accountEntry `yaml:"accounts"`
}

// accountEntry is the on-disk shape of one entry under `accounts`.
type accountEntry struct {
	// MaxParallel is how many Runs may draw on this Account at once. Unset
	// is unlimited, because the ceiling already governs burn rate.
	MaxParallel any `yaml:"maxParallel"`
	// Limits is read as what was written rather than into fields, so that a
	// ceiling Owl does not keep is refused with the two it does keep named:
	// a ceiling nobody is keeping is the one thing this block exists to
	// prevent, and the decoder's own refusal would not say which two.
	Limits map[string]any `yaml:"limits"`
}

// The ceilings an Account may be held to, which are also the only keys its
// `limits` block may carry.
const (
	fiveHourMax = "fiveHourMax"
	weeklyMax   = "weeklyMax"
)

// idlePolicy is the on-disk shape of the `idle` block.
type idlePolicy struct {
	After        string `yaml:"after"`
	RequirePower *bool  `yaml:"requirePower"`
	Interval     string `yaml:"interval"`
}

// verification is the on-disk shape of the `verification` block.
type verification struct {
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

// decodeStrict reads a file into its on-disk shape, refusing any key the shape
// does not have: a misspelled setting silently meaning the default, or a key
// that belongs to the other file kind silently doing nothing, is exactly what
// a user cannot see. whose says which file kind, for the refusal. An empty
// file decodes to nothing, which the apiVersion check then refuses by name.
func decodeStrict(source, whose string, data []byte, into any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(into)
	if errors.Is(err, io.EOF) {
		return nil
	}
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		// The decoder's own wording names a Go type; the user wrote a key in a
		// file, so that is what they are told about.
		return fmt.Errorf("%s: %s", source, unknownKeys(whose, typeErr))
	}
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	return nil
}

// unknownKeys rewords what the decoder said about keys it did not know, and
// leaves anything else it said as it was.
func unknownKeys(whose string, typeErr *yaml.TypeError) string {
	const notFound = " not found in type "
	out := make([]string, 0, len(typeErr.Errors))
	for _, msg := range typeErr.Errors {
		// "line 3: field maxParalellRuns not found in type config.projectFile"
		at, rest, found := strings.Cut(msg, ": field ")
		if key, _, ok := strings.Cut(rest, notFound); found && ok {
			// Quoted: the key comes out of a file a merged pull request can
			// change, and is printed back to a terminal, so it is shown as
			// what it is rather than as what it might do (see CheckText).
			out = append(out, fmt.Sprintf("%s: %q is not a setting in %s file", at, key, whose))
			continue
		}
		out = append(out, msg)
	}
	return strings.Join(out, "; ")
}

// Parse reads a configuration file. source names the file in error messages -
// `main:.coding-owl.yaml` for an in-repo form, an absolute path otherwise.
func Parse(source string, data []byte) (Config, error) {
	var f projectFile
	if err := decodeStrict(source, "a project's", data, &f); err != nil {
		return Config{}, err
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
	verification, err := parseVerification(source, f.Verification)
	if err != nil {
		return Config{}, err
	}
	cfg.Verification = verification
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
	// What a Project grants its Agents beyond the Account's own allowlist,
	// in the tool's own syntax, which Owl carries rather than reads
	// (ADR-0035). An entry that names nothing was meant to name something.
	for i, rule := range f.AllowedTools {
		if strings.TrimSpace(rule) == "" {
			return Config{}, fmt.Errorf("%s: allowedTools: entry %d is empty", source, i+1)
		}
		if err := CheckText("allowed tool", rule); err != nil {
			return Config{}, fmt.Errorf("%s: allowedTools: %w", source, err)
		}
		cfg.AllowedTools = append(cfg.AllowedTools, strings.TrimSpace(rule))
	}
	skills, err := parseSkills(source, f.Skills)
	if err != nil {
		return Config{}, err
	}
	cfg.Skills = skills
	parallel, err := parseParallel(source, maxParallelRuns, f.MaxParallelRuns, 1)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxParallelRuns = parallel
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

// parseVerification reads the `verification` block. A Project that does not
// mention it asks for nothing beyond its own checks, which is what a Project
// that says nothing gets.
func parseVerification(source string, v *verification) (Verification, error) {
	if v == nil {
		return Verification{}, nil
	}
	out := Verification{Agent: v.Agent}
	if v.Timeout == "" {
		return out, nil
	}
	d, err := time.ParseDuration(v.Timeout)
	if err != nil {
		return Verification{}, fmt.Errorf("%s: verification has an unreadable timeout %q: %w",
			source, v.Timeout, err)
	}
	if d <= 0 {
		return Verification{}, fmt.Errorf("%s: verification has a timeout of %s; an agent needs time to read",
			source, d)
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
		if strings.EqualFold(name, AgentVerifierName) {
			// The agent Verifier is reported beside the checks and is told apart
			// by its name, so a check may not take it (ADR-0013).
			return nil, fmt.Errorf("%s: %s is called %q, which is what the agent verifier is called",
				source, where, name)
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
	var f globalFile
	if err := decodeStrict(source, "the daemon's", data, &f); err != nil {
		return Global{}, err
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
	when, err := parseIdle(source, f.Idle)
	if err != nil {
		return Global{}, err
	}
	accounts, err := parseAccounts(source, f.Accounts)
	if err != nil {
		return Global{}, err
	}
	parallel, err := parseParallel(source, maxParallelRuns, f.MaxParallelRuns, 1)
	if err != nil {
		return Global{}, err
	}
	return Global{
		Phases:            phases,
		CredentialStore:   strings.TrimSpace(f.CredentialStore),
		GarbageCollection: collection,
		GraceWindow:       grace,
		Idle:              when,
		Accounts:          accounts,
		MaxParallelRuns:   parallel,
	}, nil
}

// The settings that say how many Runs may go at once (ADR-0021).
const (
	maxParallelRuns = "maxParallelRuns"
	maxParallel     = "maxParallel"
)

// parseParallel reads one of them: a whole number of Runs, at least one.
// Unset is whatever the caller defaults to, and zero is not a default anybody
// writes - it would mean nothing ever runs, which is what stopping the daemon
// is for.
func parseParallel(source, what string, v any, missing int) (int, error) {
	text := strings.TrimSpace(written(v))
	if v == nil || text == "" {
		return missing, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%s: %s: %q is not a number of runs", source, what, text)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s: %s: %d is not a number of runs; it is at least one, "+
			"and stopping the daemon is how nothing runs", source, what, n)
	}
	return n, nil
}

// parseAccounts reads the `accounts` block: what each Account is held to. A
// ceiling that cannot be read is refused rather than left out, because a
// ceiling nobody is keeping is the one thing this setting exists to prevent.
func parseAccounts(source string, accounts map[string]accountEntry) (map[string]Limits, error) {
	if len(accounts) == 0 {
		return nil, nil
	}
	out := map[string]Limits{}
	named := map[string]string{}
	for name, block := range accounts {
		as := strings.ToLower(strings.TrimSpace(name))
		if as == "" {
			return nil, fmt.Errorf("%s: accounts: an account with no name is held to nothing", source)
		}
		// An Account is named case-insensitively (ADR-0019), so two keys that
		// differ only in case are one Account asking for two ceilings, and
		// keeping whichever the file happened to read last is not an answer.
		if first, twice := named[as]; twice {
			return nil, fmt.Errorf(
				"%s: accounts: %q and %q are the same account, which cannot be held to two ceilings",
				source, first, name)
		}
		named[as] = name
		name = as
		parallel, err := parseParallel(source, "accounts."+name+"."+maxParallel, block.MaxParallel, 0)
		if err != nil {
			return nil, err
		}
		if len(block.Limits) == 0 {
			if parallel > 0 {
				out[name] = Limits{MaxParallel: parallel}
			}
			continue
		}
		// In a fixed order, so that a file with more than one thing wrong with
		// it is always refused for the same one.
		l := Limits{MaxParallel: parallel}
		for _, what := range []string{fiveHourMax, weeklyMax} {
			value, set := block.Limits[what]
			if set && strings.TrimSpace(written(value)) == "" {
				return nil, fmt.Errorf(
					"%s: accounts.%q.limits.%s: nothing was written after it; leave it out for no ceiling",
					source, name, what)
			}
			pct, err := parsePercent(source, name, what, written(value))
			if err != nil {
				return nil, err
			}
			switch what {
			case fiveHourMax:
				l.FiveHourMax = pct
			case weeklyMax:
				l.WeeklyMax = pct
			}
		}
		unknown := make([]string, 0, len(block.Limits))
		for what := range block.Limits {
			if what != fiveHourMax && what != weeklyMax {
				unknown = append(unknown, what)
			}
		}
		sort.Strings(unknown)
		if len(unknown) > 0 {
			return nil, fmt.Errorf(
				"%s: accounts.%q.limits: %q is not a ceiling Owl keeps; it keeps %s and %s",
				source, name, unknown[0], fiveHourMax, weeklyMax)
		}
		out[name] = l
	}
	return out, nil
}

// written is a ceiling as the file wrote it, whether that was a number or a
// string with a sign on it.
func written(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// parsePercent reads a ceiling: a number of percent, written with or without
// the sign. Zero is nothing set rather than a ceiling of nothing, which would
// be a way of saying never run at all.
func parsePercent(source, account, what, value string) (float64, error) {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "%"))
	if value == "" {
		return 0, nil
	}
	pct, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: accounts.%q.limits.%s: %q is not a percentage like 60",
			source, account, what, value)
	}
	if pct <= 0 || pct > 100 {
		return 0, fmt.Errorf(
			"%s: accounts.%q.limits.%s: %q is not a share of a window; it is between 1 and 100",
			source, account, what, value)
	}
	return pct, nil
}

// minInterval is as often as Owl will look at the machine. Every look runs the
// tools that read it, so a configuration asking for oftener than this is
// asking for a machine spent on watching itself.
const minInterval = 50 * time.Millisecond

// parseIdle reads the `idle` block: when the machine counts as one nobody is
// at, and how often Owl looks. A policy that cannot be read is refused rather
// than quietly replaced with the default, which would have Owl working at a
// time nobody asked for.
func parseIdle(source string, p *idlePolicy) (Idle, error) {
	if p == nil {
		return Idle{}, nil
	}
	out := Idle{RequirePower: p.RequirePower}
	for what, field := range map[string]struct {
		value string
		into  *time.Duration
	}{
		"after":    {p.After, &out.After},
		"interval": {p.Interval, &out.Interval},
	} {
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		d, err := time.ParseDuration(field.value)
		if err != nil {
			return Idle{}, fmt.Errorf("%s: idle.%s: %q is not a duration like 10m", source, what, field.value)
		}
		if d <= 0 {
			return Idle{}, fmt.Errorf(
				"%s: idle.%s: %s is not a length of time Owl can wait; leave it out for the default",
				source, what, field.value)
		}
		// Looking at the machine costs a look, and looking oftener than this
		// is a loop rather than a policy.
		if what == "interval" && d < minInterval {
			return Idle{}, fmt.Errorf(
				"%s: idle.interval: %s is oftener than Owl looks at a machine; %s is as often as it goes",
				source, field.value, minInterval)
		}
		*field.into = d
	}
	return out, nil
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
