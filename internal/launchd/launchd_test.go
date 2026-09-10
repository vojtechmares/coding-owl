package launchd_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/launchd"
)

// env answers from a map, standing in for the process environment.
func env(kv map[string]string) func(string) string {
	return func(name string) string { return kv[name] }
}

func TestDescribeCarriesOnlyTheVariablesThatAreSet(t *testing.T) {
	a := launchd.Describe("/opt/owl", "/state/coding-owl", env(map[string]string{
		"PATH":            "/usr/bin",
		"XDG_STATE_HOME":  "/state",
		"XDG_CONFIG_HOME": "",
		"HOME":            "/home/someone",
	}))

	if got, want := a.Arguments, []string{"/opt/owl", "daemon", "run"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("arguments = %v, want %v", got, want)
	}
	if a.LogPath != filepath.Join("/state/coding-owl", "daemon.log") {
		t.Errorf("log path = %s, want it inside the state directory", a.LogPath)
	}
	var names []string
	for _, v := range a.Environment {
		names = append(names, v.Name)
	}
	if strings.Join(names, ",") != "PATH,XDG_STATE_HOME" {
		t.Errorf("the agent carries %v; it carries what is set, and nothing else", names)
	}
}

func TestRenderEscapesWhatWouldBeMarkup(t *testing.T) {
	a := launchd.Describe("/opt/tools & toys/owl", "/state/coding-owl", env(map[string]string{
		"PATH": "/usr/bin:/opt/<odd>/bin",
	}))

	body, err := launchd.Render(a)

	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"<string>/opt/tools &amp; toys/owl</string>",
		"/opt/&lt;odd&gt;/bin",
		"<string>dev.codingowl.owld</string>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the agent does not carry %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "toys & owl") || strings.Contains(body, "<odd>") {
		t.Errorf("a value went into the property list unescaped:\n%s", body)
	}
	if runtime.GOOS != "darwin" {
		return
	}
	// plutil is the authority on whether what was rendered is a property list.
	cmd := exec.Command("plutil", "-lint", "-")
	cmd.Stdin = strings.NewReader(body)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("plutil refuses the rendered agent: %v\n%s\n%s", err, out, body)
	}
}

func TestPathIsTheUsersOwnLaunchAgentsDirectory(t *testing.T) {
	if got, want := launchd.Path("/home/someone"), "/home/someone/Library/LaunchAgents/dev.codingowl.owld.plist"; got != want {
		t.Errorf("Path = %s, want %s", got, want)
	}
}
