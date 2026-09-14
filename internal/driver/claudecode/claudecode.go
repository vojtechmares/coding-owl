// Package claudecode drives Claude Code (ADR-0012, ADR-0018). Every
// Claude-specific flag lives here: print mode, the stream-json output format,
// the unattended permission posture, the spend cap and the appended system
// prompt. Owl talks to two narrow surfaces of a CLI that carries no stability
// guarantee, so the supported version range is pinned and a mismatch refuses
// to start rather than guessing.
package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/driver"
)

// program is the Claude Code binary, found on PATH like any other tool.
const program = "claude"

// configDirEnv relocates Claude Code's entire configuration and credential
// state, which is what gives an Account a setup of its own (ADR-0019).
const configDirEnv = "CLAUDE_CONFIG_DIR"

// tokenEnv is the long-lived token `claude setup-token` prints, which is how
// Claude Code authenticates without a browser.
const tokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

// skillsDir is where Claude Code reads Skills from, relative to the directory
// it runs in. It is not the plugins directory, which is a different thing
// (ADR-0033).
const skillsDir = ".claude/skills"

// MinVersion is the oldest Claude Code Owl drives, and NextMajor the release
// it stops at. The flags below are a CLI contract rather than an API, so the
// range is pinned and checked before every Run (ADR-0012).
const (
	MinVersion = "2.1.0"
	NextMajor  = "3.0.0"
)

// versionTimeout bounds `claude --version`, so a wedged binary cannot hold a
// Run open before it has started.
const versionTimeout = 30 * time.Second

// Driver builds Claude Code invocations.
type Driver struct{}

// New returns the Claude Code Driver.
func New() *Driver { return &Driver{} }

// Name identifies the Driver.
func (*Driver) Name() string { return "claude-code" }

// Capabilities is what Claude Code can do.
func (*Driver) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		StreamingOutput: true,
		BudgetCap:       true,
		PermissionModes: true,
		UsageReporting:  true,
	}
}

// usageSubtype is the system event that carries the account's limits.
const usageSubtype = "usage_limits"

// maxUtilization is the largest figure that is a percentage of a window. One
// above it is a tool saying something Owl does not understand, and a ceiling
// kept against a number nobody can read is no ceiling at all.
const maxUtilization = 100

// maxWindows is how many windows one line may be about, and maxWindowName how
// long a window may be called. A tool reports a handful of windows with names
// of its own; these bound what a tool that does something else costs Owl to
// read.
const (
	maxWindows    = 16
	maxWindowName = 64
)

// usageEvent is the shape of that line: a window each, with how much of it is
// spent and when it starts again.
type usageEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Limits  map[string]struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	} `json:"limits"`
}

// Usage reads what a line of Claude Code's stream says about the account's
// utilization (ADR-0020). A line that is not that event, or that carries
// nothing readable, says nothing: one odd line is not a reason to end a Run,
// and a window Owl cannot read is one it does not pretend to know.
func (*Driver) Usage(line string) (driver.Usage, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") || !strings.Contains(line, usageSubtype) {
		return driver.Usage{}, false
	}
	var ev usageEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return driver.Usage{}, false
	}
	if ev.Type != "system" || ev.Subtype != usageSubtype {
		return driver.Usage{}, false
	}
	if len(ev.Limits) > maxWindows {
		return driver.Usage{}, false
	}
	var out driver.Usage
	for name, limit := range ev.Limits {
		if name == "" || len(name) > maxWindowName {
			continue
		}
		if limit.Utilization == nil || *limit.Utilization < 0 || *limit.Utilization > maxUtilization {
			continue
		}
		resets, err := time.Parse(time.RFC3339, limit.ResetsAt)
		if err != nil {
			continue
		}
		out.Windows = append(out.Windows, driver.UsageWindow{
			Name: name, Utilization: *limit.Utilization, Resets: resets.UTC(),
		})
	}
	if len(out.Windows) == 0 {
		return driver.Usage{}, false
	}
	// By name, so that a reading reads the same twice: what a map does with
	// them is not an order.
	sort.Slice(out.Windows, func(i, j int) bool { return out.Windows[i].Name < out.Windows[j].Name })
	return out, true
}

// SkillsDir is where Claude Code reads Skills from inside a worktree.
func (*Driver) SkillsDir() string { return skillsDir }

// Check reports whether Claude Code is installed and a version Owl drives.
func (d *Driver) Check(ctx context.Context) error {
	path, err := lookPath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return fmt.Errorf("asking %s for its version: %w", path, err)
	}
	found, err := parseVersion(string(out))
	if err != nil {
		return err
	}
	if !supported(found) {
		return fmt.Errorf("%s is %s, which Owl does not drive: it supports %s or later, before %s",
			path, found, MinVersion, NextMajor)
	}
	return nil
}

// Command builds the Agent for one Run. The Agent prints rather than talks,
// streams structured events as it works, and denies anything that would prompt
// for permission - nobody is awake to answer (ADR-0012, ADR-0017).
func (d *Driver) Command(req driver.Request) (agent.Invocation, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return agent.Invocation{}, errors.New("a run needs a prompt")
	}
	path, err := lookPath()
	if err != nil {
		return agent.Invocation{}, err
	}
	// What the Agent may do is granted in the tool's own settings, seeded
	// once per Account and the user's from then on, plus whatever the
	// Project adds for its own Agents (ADR-0006, ADR-0035). Denying every
	// prompt only makes sense once something has been granted.
	granted, err := ensureSettings(req.ConfigDir)
	if err != nil {
		return agent.Invocation{}, err
	}
	extra, err := allowedTools(req.AllowedTools)
	if err != nil {
		return agent.Invocation{}, err
	}
	args := []string{
		"--print",
		// stream-json emits nothing useful without --verbose.
		"--verbose",
		"--output-format", "stream-json",
		// none: anything that would prompt is denied rather than approved.
		"--permission-prompts", "none",
	}
	if len(extra) > 0 {
		args = append(args, "--allowedTools", strings.Join(extra, ","))
	}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if req.BudgetUSD > 0 {
		args = append(args, "--max-budget-usd", formatUSD(req.BudgetUSD))
	}
	// A phase decides what the Agent runs as and how hard it thinks
	// (ADR-0028). Both are values, never options, so a dash-leading one is
	// refused rather than passed on.
	for _, setting := range []struct{ flag, value string }{
		{"--model", req.Model},
		{"--effort", req.Effort},
	} {
		if setting.value == "" {
			continue
		}
		if strings.HasPrefix(setting.value, "-") {
			return agent.Invocation{}, fmt.Errorf("%s %q may not start with a dash", setting.flag, setting.value)
		}
		args = append(args, setting.flag, setting.value)
	}
	// The prompt is the user's text and may begin with a dash, so options end
	// before it.
	args = append(args, "--", req.Prompt)
	env, err := accountEnv(req.ConfigDir, req.Token)
	if err != nil {
		return agent.Invocation{}, err
	}
	return agent.Invocation{
		Path: path, Args: args, Dir: req.WorkingDir, Env: env, Unset: daemonCredentials,
		Permissions: append(granted, extra...),
	}, nil
}

// SetupToken builds `claude setup-token`, which walks the user through
// authorising a subscription and prints a long-lived token. It runs against
// the Account's own configuration directory, so the flow never touches the
// user's own (ADR-0019).
func (d *Driver) SetupToken(configDir string) (agent.Invocation, error) {
	path, err := lookPath()
	if err != nil {
		return agent.Invocation{}, err
	}
	env, err := accountEnv(configDir, "")
	if err != nil {
		return agent.Invocation{}, err
	}
	// The setup runs in the Account's own directory, not in whatever directory
	// the user happened to type the command in: it is the tool's own flow, and
	// an Agent belongs where its work is (ADR-0006, ADR-0019).
	return agent.Invocation{Path: path, Args: []string{"setup-token"}, Dir: configDir, Env: env, Unset: daemonCredentials}, nil
}

// settingsName is the tool's own settings file inside a configuration
// directory, which is where it reads what it is allowed to do.
const settingsName = "settings.json"

// DefaultAllowedTools is what an Account's Agents are granted when nothing
// has been decided for that Account yet: the edits an Agent exists to make,
// reading what is there, and the commands a Job's work turns on - version
// control and the build and test runners a Project is likely to have. It is
// written once into a fresh Account's settings file, which is the user's from
// then on (ADR-0035). Anything wider is the Project's to add.
var DefaultAllowedTools = []string{
	"Edit", "Write", "MultiEdit", "NotebookEdit",
	"Read", "Glob", "Grep", "LS",
	"Bash(git:*)", "Bash(make:*)", "Bash(go:*)",
	"Bash(npm:*)", "Bash(pnpm:*)", "Bash(yarn:*)", "Bash(npx:*)",
	"Bash(cargo:*)", "Bash(pytest:*)",
}

// settings is the part of the tool's settings file Owl writes and reads.
type settings struct {
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
}

// ensureSettings makes sure an Account's configuration directory holds a
// settings file, seeding the default allowlist into one that is not there,
// and returns what the file allows. A file that exists is the user's, whoever
// wrote it, and is read rather than written: a default is a starting point,
// not a setting Owl keeps re-imposing (ADR-0035).
func ensureSettings(configDir string) ([]string, error) {
	if configDir == "" {
		return nil, errors.New("a run needs an account's configuration directory")
	}
	path := filepath.Join(configDir, settingsName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return seedSettings(configDir, path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading the account's %s: %w", settingsName, err)
	}
	var have settings
	if err := json.Unmarshal(data, &have); err != nil {
		return nil, fmt.Errorf("the account's %s is not what the tool reads: %w", path, err)
	}
	return have.Permissions.Allow, nil
}

// seedSettings writes the default allowlist into a fresh Account's settings
// file. The file is created rather than written over, so two Runs starting at
// once cannot both seed it, and one the user made in between is left alone.
func seedSettings(configDir, path string) ([]string, error) {
	// The directory is the Account's alone, as everything in it is
	// (ADR-0019).
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, err
	}
	var seed settings
	seed.Permissions.Allow = DefaultAllowedTools
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		// Somebody got there first, and what they wrote is what counts.
		return ensureSettings(configDir)
	}
	if err != nil {
		return nil, fmt.Errorf("seeding the account's %s: %w", settingsName, err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("seeding the account's %s: %w", settingsName, err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("seeding the account's %s: %w", settingsName, err)
	}
	return slices.Clone(DefaultAllowedTools), nil
}

// allowedTools is what a Project adds to its Agents' allowlist, checked the
// way the tool will read it: each rule is one argument's worth of text, and
// the list is passed as one comma-separated value, so a rule may hold neither
// a comma nor anything that would not print as itself.
func allowedTools(rules []string) ([]string, error) {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		if strings.ContainsAny(rule, ",\x00\n\r") {
			return nil, fmt.Errorf("allowed tool %q may not hold a comma or a line break", rule)
		}
		if strings.HasPrefix(rule, "-") {
			return nil, fmt.Errorf("allowed tool %q may not start with a dash", rule)
		}
		out = append(out, rule)
	}
	return out, nil
}

// daemonCredentials are the variables the tool takes a credential from, which
// a daemon started from a shell may carry from the user's own setup. Each of
// them is kept from every Agent: an API key would be used in place of any
// Account's token, and a token would fill in for an Account that has none.
// An Account draws on its own credential and nothing else (ADR-0019).
var daemonCredentials = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", tokenEnv}

// accountEnv is what an Account adds to the environment an Agent runs in: the
// configuration directory that keeps it apart from every other Account, and
// the token it draws on. An empty token is left out rather than set empty,
// which the tool would read as an account with no subscription.
func accountEnv(configDir, token string) ([]string, error) {
	if configDir == "" {
		return nil, errors.New("a run needs an account's configuration directory")
	}
	if strings.ContainsAny(configDir, "\x00\n") || strings.ContainsAny(token, "\x00\n") {
		return nil, errors.New("an account's directory and token may not hold a newline or a null")
	}
	env := []string{configDirEnv + "=" + configDir}
	if token != "" {
		env = append(env, tokenEnv+"="+token)
	}
	return env, nil
}

// lookPath finds Claude Code, and says so plainly when it is not there.
func lookPath() (string, error) {
	path, err := exec.LookPath(program)
	if err != nil {
		return "", fmt.Errorf("%s is not installed, or not on the daemon's PATH: %w", program, err)
	}
	return path, nil
}

// formatUSD renders a budget the way a person wrote it, so `5` does not become
// `5.000000` in the argv a user reads back.
func formatUSD(usd float64) string {
	return strconv.FormatFloat(usd, 'f', -1, 64)
}

// version is a three-part release number, which is all the comparison below
// needs; Claude Code has never shipped anything else.
type version struct{ major, minor, patch int }

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

// compare orders two versions the way their numbers order.
func (v version) compare(o version) int {
	for _, pair := range [][2]int{{v.major, o.major}, {v.minor, o.minor}, {v.patch, o.patch}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// parseVersion reads the number out of `claude --version`, which prints
// something like `2.1.267 (Claude Code)`.
func parseVersion(out string) (version, error) {
	field, _, _ := strings.Cut(strings.TrimSpace(out), " ")
	parts := strings.Split(field, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("cannot read a version out of %q", strings.TrimSpace(out))
	}
	var v version
	for i, into := range []*int{&v.major, &v.minor, &v.patch} {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return version{}, fmt.Errorf("cannot read a version out of %q", strings.TrimSpace(out))
		}
		*into = n
	}
	return v, nil
}

// supported reports whether Owl drives that version.
func supported(v version) bool {
	min, err := parseVersion(MinVersion)
	if err != nil {
		return false
	}
	next, err := parseVersion(NextMajor)
	if err != nil {
		return false
	}
	return v.compare(min) >= 0 && v.compare(next) < 0
}
