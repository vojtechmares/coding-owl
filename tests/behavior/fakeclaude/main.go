// Command fakeclaude stands in for Claude Code in the behavior tests. The
// harness builds it as `claude` into a directory it puts first on the daemon's
// PATH, so the Claude Code Driver finds it exactly where it would find the real
// one - and no test spends a token.
//
// It is configured entirely through the environment it inherits from the
// daemon:
//
//	OWL_FAKE_CLAUDE_VERSION  what --version prints
//	OWL_FAKE_CLAUDE_ARGV     file to record the invocation in, as JSON
//	OWL_FAKE_CLAUDE_SCRIPT   file of lines to emit on stdout, one per line
//	OWL_FAKE_CLAUDE_WAIT     file whose appearance releases a `#wait` line
//	OWL_FAKE_CLAUDE_EXIT     exit status, default 0
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
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
	data, err := json.Marshal(invocation{Argv: os.Args, PID: os.Getpid(), PPID: os.Getppid(), Dir: dir})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
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
