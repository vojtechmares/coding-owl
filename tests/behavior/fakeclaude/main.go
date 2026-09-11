// Command fakeclaude stands in for Claude Code in the behavior tests. The
// harness builds it as `claude` into a directory it puts first on the daemon's
// PATH, so the Claude Code Driver finds it exactly where it would find the real
// one - and no test spends a token.
//
// It is configured entirely through the environment it inherits from the
// daemon:
//
//	OWL_FAKE_CLAUDE_VERSION  what --version prints
//	OWL_FAKE_CLAUDE_ARGV     file to append the invocation to, one JSON object
//	                         per line, so several runs each leave a record
//	OWL_FAKE_CLAUDE_SCRIPT   file of lines to emit on stdout, one per line
//	OWL_FAKE_CLAUDE_WAIT     file whose appearance releases a `#wait` line
//	OWL_FAKE_CLAUDE_EXIT     exit status, default 0
//	OWL_FAKE_CLAUDE_WRITE    JSON object of path to contents, written into the
//	                         working directory before the script is emitted
//	OWL_FAKE_CLAUDE_COMMIT   when set, commits what was written
//	OWL_FAKE_CLAUDE_GIT      JSON array of git argument arrays, run in the
//	                         working directory after the files are written
//
// The invocation record also holds the names of the files in the working
// directory, so a scenario can see what ran before the Agent did, and every
// CLAUDE_ variable it was given, so a scenario can see which Account it was
// run as (ADR-0019).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// waitLine holds the script until the release file appears, so a scenario can
// keep a Run in progress for as long as it needs.
const waitLine = "#wait"

// waitTimeout bounds that hold, so a forgotten release file fails the scenario
// rather than hanging it.
const waitTimeout = 30 * time.Second

// invocation is what the stub records about how it was called.
type invocation struct {
	Argv []string `json:"argv"`
	PID  int      `json:"pid"`
	PPID int      `json:"ppid"`
	Dir  string   `json:"dir"`
	// Entries are the names in the working directory when the stub started,
	// which is how a scenario sees what a setup command left there.
	Entries []string `json:"entries"`
	// Env holds the CLAUDE_ variables the stub was given, which is how a
	// scenario sees the Account a Run was made on.
	Env map[string]string `json:"env"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(env("OWL_FAKE_CLAUDE_VERSION", "2.1.267 (Claude Code)"))
		return
	}
	if path := os.Getenv("OWL_FAKE_CLAUDE_ARGV"); path != "" {
		if err := record(path); err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude:", err)
			os.Exit(90)
		}
	}
	if err := writeFiles(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude:", err)
		os.Exit(93)
	}
	if err := runGit(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude:", err)
		os.Exit(94)
	}
	if script := os.Getenv("OWL_FAKE_CLAUDE_SCRIPT"); script != "" {
		if err := emit(script); err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude:", err)
			os.Exit(91)
		}
	}
	code, err := strconv.Atoi(env("OWL_FAKE_CLAUDE_EXIT", "0"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: OWL_FAKE_CLAUDE_EXIT:", err)
		os.Exit(92)
	}
	os.Exit(code)
}

// refuseOwlsOwnCheckout fails when the stub was started inside the repository
// it is part of, which it can tell by the file only that repository has. A
// probe that only reports a version is harmless anywhere; writing is not.
func refuseOwlsOwnCheckout() error {
	started, err := os.Getwd()
	if err != nil {
		return err
	}
	for dir := started; ; {
		if _, err := os.Stat(filepath.Join(dir, "tests", "behavior", "fakeclaude", "main.go")); err == nil {
			return fmt.Errorf("started in %s, which is inside Owl's own checkout at %s; an agent belongs in a job's worktree",
				started, dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func record(path string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	entries, err := entryNames(dir)
	if err != nil {
		return err
	}
	data, err := json.Marshal(invocation{
		Argv: os.Args, PID: os.Getpid(), PPID: os.Getppid(), Dir: dir, Entries: entries,
		Env: claudeEnv(),
	})
	if err != nil {
		return err
	}
	// Appended rather than written: a Job takes several runs, and each one's
	// invocation is worth reading back.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(data, '\n'))
	return err
}

// claudeEnv is every CLAUDE_ variable the stub was given. Only that prefix is
// recorded: the record is a file in a temporary directory, and the rest of the
// environment is nobody's business.
func claudeEnv() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		name, value, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(name, "CLAUDE_") {
			out[name] = value
		}
	}
	return out
}

// entryNames lists what is in the working directory, sorted so a scenario can
// read it back without caring about order.
func entryNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// writeFiles puts the files of OWL_FAKE_CLAUDE_WRITE in the working directory,
// which is how a scripted Agent leaves a handoff behind, and commits them when
// OWL_FAKE_CLAUDE_COMMIT is set.
func writeFiles() error {
	spec := os.Getenv("OWL_FAKE_CLAUDE_WRITE")
	if spec == "" {
		return nil
	}
	// The files go in whatever directory the stub was started in, so it
	// refuses to write in Owl's own checkout: an Agent started without a
	// working directory would otherwise write, and commit, into the repository
	// it is testing.
	if err := refuseOwlsOwnCheckout(); err != nil {
		return err
	}
	var files map[string]string
	if err := json.Unmarshal([]byte(spec), &files); err != nil {
		return fmt.Errorf("OWL_FAKE_CLAUDE_WRITE: %w", err)
	}
	paths := make([]string, 0, len(files))
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
		paths = append(paths, path)
	}
	if os.Getenv("OWL_FAKE_CLAUDE_COMMIT") == "" {
		return nil
	}
	sort.Strings(paths)
	if err := git(append([]string{"add", "--"}, paths...)); err != nil {
		return err
	}
	// A run that wrote what was already there has nothing to commit, which is
	// not a failure.
	if err := git(append([]string{"diff", "--cached", "--quiet", "--"}, paths...)); err == nil {
		return nil
	}
	// The paths are named, so a stub that finds itself running somewhere it
	// was not meant to - a working directory nobody set, which is whatever
	// the process inherited - commits what it wrote and nothing else.
	return git(append([]string{"commit", "-m", "the agent's own commit", "--"}, paths...))
}

// runGit runs the git commands of OWL_FAKE_CLAUDE_GIT, which is how a scenario
// has the Agent do something to the repository beyond its own commit.
func runGit() error {
	spec := os.Getenv("OWL_FAKE_CLAUDE_GIT")
	if spec == "" {
		return nil
	}
	var commands [][]string
	if err := json.Unmarshal([]byte(spec), &commands); err != nil {
		return fmt.Errorf("OWL_FAKE_CLAUDE_GIT: %w", err)
	}
	for _, args := range commands {
		if err := git(args); err != nil {
			return err
		}
	}
	return nil
}

// git runs a git command in the working directory with an identity of its own,
// since the test environment deliberately has no git configuration.
func git(args []string) error {
	cmd := exec.Command("git", append([]string{
		"-c", "user.name=Fake Agent", "-c", "user.email=agent@example.com",
	}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

// emit writes the script's lines to stdout as they are read, unbuffered, so a
// scenario following the log sees each one as it happens.
func emit(script string) error {
	f, err := os.Open(script)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == waitLine {
			if err := waitForRelease(); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(os.Stdout, line); err != nil {
			return err
		}
	}
	return sc.Err()
}

func waitForRelease() error {
	path := os.Getenv("OWL_FAKE_CLAUDE_WAIT")
	if path == "" {
		return fmt.Errorf("a script asked to wait but OWL_FAKE_CLAUDE_WAIT is unset")
	}
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("waited %s for %s and it never appeared", waitTimeout, path)
}
