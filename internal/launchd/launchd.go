// Package launchd installs Coding Owl's daemon as a launchd user agent, for
// machines that did not get it from Homebrew. The daemon still runs in the
// foreground and is still supervised by somebody else (ADR-0002); this only
// writes the unit that says so and hands it to launchd.
package launchd

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/vojtechmares/coding-owl/deploy"
)

// Label is the agent's launchd label, and the name of the file it is written
// to.
const Label = "dev.codingowl.owld"

// logName is where the agent's output goes, inside the state directory the
// daemon already owns.
const logName = "daemon.log"

// Variable is one entry of the agent's environment.
type Variable struct {
	Name  string
	Value string
}

// Agent is what the unit says: what to run, where its output goes, and what it
// runs with.
type Agent struct {
	Label       string
	Arguments   []string
	LogPath     string
	Environment []Variable
}

// carried are the variables the agent needs to see the same layout the person
// installing it sees. launchd hands a user agent almost nothing, so a shell
// with XDG variables set would otherwise get a daemon looking somewhere else
// (ADR-0014), and PATH is how the daemon finds git and the Agent.
var carried = []string{
	"PATH",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_STATE_HOME",
	"XDG_RUNTIME_DIR",
}

// Describe builds the agent that runs owl at program with the layout stateDir
// belongs to. getenv reads the environment the variables are taken from.
func Describe(program, stateDir string, getenv func(string) string) Agent {
	a := Agent{
		Label:     Label,
		Arguments: []string{program, "daemon", "run"},
		LogPath:   filepath.Join(stateDir, logName),
	}
	for _, name := range carried {
		if v := getenv(name); v != "" {
			a.Environment = append(a.Environment, Variable{Name: name, Value: v})
		}
	}
	return a
}

var tmpl = template.Must(template.New("agent").Funcs(template.FuncMap{
	// Paths can hold characters a property list would read as markup, so
	// every value goes through the escaper rather than straight into the
	// document.
	"xml": func(s string) (string, error) {
		var b bytes.Buffer
		if err := xml.EscapeText(&b, []byte(s)); err != nil {
			return "", err
		}
		return b.String(), nil
	},
}).Parse(deploy.LaunchdAgent))

// Render returns the property list for an agent.
func Render(a Agent) (string, error) {
	var b strings.Builder
	if err := tmpl.Execute(&b, a); err != nil {
		return "", fmt.Errorf("rendering the launch agent: %w", err)
	}
	return b.String(), nil
}

// Path is where the agent is written for a user whose home directory is home.
func Path(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

// domain is the launchd domain a user agent belongs to.
func domain(uid int) string { return fmt.Sprintf("gui/%d", uid) }

// Install writes the agent and hands it to launchd, replacing whatever was
// there before: installing twice is how a person moves the daemon to a new
// binary, so it must not be an error.
func Install(a Agent, home string, uid int) (string, error) {
	body, err := Render(a)
	if err != nil {
		return "", err
	}
	path := Path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// The daemon's own directory has to exist before launchd opens the log it
	// is told to write, or the agent fails to spawn.
	if err := os.MkdirAll(filepath.Dir(a.LogPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	// An agent that is not loaded makes this fail, which is the ordinary case
	// on a first install and is not something to report.
	_ = exec.Command("launchctl", "bootout", domain(uid)+"/"+a.Label).Run()

	out, err := exec.Command("launchctl", "bootstrap", domain(uid), path).CombinedOutput()
	if err != nil {
		return path, fmt.Errorf("launchctl could not load %s: %w\n%s", path, err, strings.TrimSpace(string(out)))
	}
	return path, nil
}
