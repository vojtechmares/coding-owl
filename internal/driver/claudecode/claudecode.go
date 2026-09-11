// Package claudecode drives Claude Code (ADR-0012, ADR-0018). Every
// Claude-specific flag lives here: print mode, the stream-json output format,
// the unattended permission posture, the spend cap and the appended system
// prompt. Owl talks to two narrow surfaces of a CLI that carries no stability
// guarantee, so the supported version range is pinned and a mismatch refuses
// to start rather than guessing.
package claudecode

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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

// Capabilities is what Claude Code can do. Usage reporting is false until Owl
// reads utilization out of the stream (ADR-0020).
func (*Driver) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		StreamingOutput: true,
		BudgetCap:       true,
		PermissionModes: true,
		UsageReporting:  false,
	}
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
	args := []string{
		"--print",
		// stream-json emits nothing useful without --verbose.
		"--verbose",
		"--output-format", "stream-json",
		// none: anything that would prompt is denied rather than approved.
		"--permission-prompts", "none",
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
	return agent.Invocation{Path: path, Args: args, Dir: req.WorkingDir, Env: env}, nil
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
	return agent.Invocation{Path: path, Args: []string{"setup-token"}, Dir: configDir, Env: env}, nil
}

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
