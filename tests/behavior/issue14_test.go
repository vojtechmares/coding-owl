package behavior_test

// Behavior tests for issue #14. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-14.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// whose credentials go in a file rather than in a keychain, so that no test
// touches a real one.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testToken stands in for what `claude setup-token` prints.
const testToken = "sk-ant-oat01-behavior-test"

// fileStore is the daemon configuration that keeps credentials in a file.
const fileStore = "apiVersion: codingowl.dev/v1\ncredentialStore: file\n"

// accountLayout is an XDG layout with the stub agent first on the PATH of both
// the daemon and the commands, and credentials kept in a file.
func accountLayout(t *testing.T) (*layout, *stub) {
	t.Helper()
	l, s := agentLayout(t, agentScript, 0)
	globalConfig(t, l, fileStore)
	return l, s
}

// runOwlStdin runs owl with input on its standard input, which is how a token
// is pasted.
func runOwlStdin(t *testing.T, l *layout, input string, args ...string) result {
	t.Helper()
	cmd := exec.Command(owlBin, args...)
	cmd.Env = l.env
	cmd.Stdin = strings.NewReader(input)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running owl %v: %v", args, err)
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

// addAccount adds an Account with its token pasted on stdin.
func addAccount(t *testing.T, l *layout, name, token string, flags ...string) result {
	t.Helper()
	args := append([]string{"account", "add", name, "--token-stdin"}, flags...)
	res := runOwlStdin(t, l, token, args...)
	if res.code != 0 {
		t.Fatalf("owl %v exited %d\nstdout:\n%s\nstderr:\n%s", args, res.code, res.stdout, res.stderr)
	}
	return res
}

// accountDir is where an Account's own tool configuration lives (ADR-0019).
func accountDir(l *layout, name string) string {
	return filepath.Join(l.data, "coding-owl", "accounts", name)
}

// credentialPath is the file the daemon keeps credentials in when it is
// configured to use one.
func credentialPath(l *layout) string {
	return filepath.Join(l.data, "coding-owl", "credentials.json")
}

// credentials is what that file holds, keyed by credential reference.
func credentials(t *testing.T, l *layout) map[string]string {
	t.Helper()
	data, err := os.ReadFile(credentialPath(l))
	if err != nil {
		t.Fatalf("reading the credential store: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("the credential store is unreadable: %v\n%s", err, data)
	}
	return out
}

// accountRow is one row of owl account list.
type accountRow struct{ name, driver, credential, failover, dir string }

// accountRows parses owl account list, skipping its header.
func accountRows(t *testing.T, l *layout) []accountRow {
	t.Helper()
	out := mustOwl(t, l, "account", "list").stdout
	var rows []accountRow
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 || f[0] == "NAME" {
			continue
		}
		rows = append(rows, accountRow{name: f[0], driver: f[1], credential: f[2], failover: f[3], dir: f[4]})
	}
	return rows
}

// accountProject registers a Project whose configuration names an Account, and
// queues one Job against it.
func accountProject(t *testing.T, l *layout, name, config string) *repo {
	t.Helper()
	r := newRepo(t, l, name)
	if config != "" {
		r.commit(".coding-owl.yaml", config, "configure owl")
	}
	addProject(t, l, r)
	addJob(t, l, r.dir, "do the thing", "--no-plan")
	return r
}

func TestS1AccountAddCreatesTheAccountAndItsDirectory(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)

	res := addAccount(t, l, "work", testToken)

	dir := accountDir(l, "work")
	for _, want := range []string{"work", dir} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("owl account add does not name %q:\n%s", want, res.stdout)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the account's configuration directory: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", dir)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("the account's directory is %o, want 700: it holds a tool's credentials", perm)
	}
	rows := accountRows(t, l)
	if len(rows) != 1 {
		t.Fatalf("owl account list reports %d accounts, want one:\n%+v", len(rows), rows)
	}
	if rows[0].name != "work" || rows[0].driver != "claude-code" || rows[0].credential != "yes" {
		t.Errorf("owl account list reports %+v, want work on claude-code with a credential", rows[0])
	}
}

func TestS2TheSecretIsInNeitherTheDatabaseNorTheLog(t *testing.T) {
	l, _ := accountLayout(t)
	d := daemonUp(t, l)

	addAccount(t, l, "work", testToken)

	db, err := os.ReadFile(filepath.Join(l.data, "coding-owl", "owl.db"))
	if err != nil {
		t.Fatalf("reading the database: %v", err)
	}
	if bytes.Contains(db, []byte(testToken)) {
		t.Error("the database holds the token itself, not a reference to it")
	}
	if strings.Contains(d.out(), testToken) {
		t.Errorf("the daemon logged the token:\n%s", d.out())
	}
	if !bytes.Contains(db, []byte("account/work")) {
		t.Error("the database holds no reference to the account's credential")
	}
}

func TestS3TheCredentialStoreIsReadableOnlyByItsOwner(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)

	addAccount(t, l, "work", testToken)

	info, err := os.Stat(credentialPath(l))
	if err != nil {
		t.Fatalf("the credential store: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the credential store is %o, want 600: it holds a secret", perm)
	}
	if got := credentials(t, l)["account/work"]; got != testToken {
		t.Errorf("the credential store holds %q for the account, want the token it was given", got)
	}
}

func TestS4AccountAddWalksThroughTheToolsTokenSetup(t *testing.T) {
	l, s := accountLayout(t)
	daemonUp(t, l)

	res := runOwlStdin(t, l, "pasted-token\n", "account", "add", "work")

	if res.code != 0 {
		t.Fatalf("owl account add exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	inv := s.invoked(t)
	if !slices.Contains(inv.Argv, "setup-token") {
		t.Errorf("the tool was invoked as %v, want its token setup", inv.Argv)
	}
	if got := inv.Env["CLAUDE_CONFIG_DIR"]; got != accountDir(l, "work") {
		t.Errorf("the token setup ran with CLAUDE_CONFIG_DIR=%q, want the account's own directory %q",
			got, accountDir(l, "work"))
	}
	if got := credentials(t, l)["account/work"]; got != "pasted-token" {
		t.Errorf("the account's credential is %q, want what was pasted", got)
	}
}

func TestS5AccountAddRefusesANameThatIsTaken(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)

	res := runOwlStdin(t, l, testToken, "account", "add", "work", "--token-stdin")

	if res.code == 0 {
		t.Fatalf("adding an account twice exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "work") {
		t.Errorf("stderr does not name the account:\n%s", res.stderr)
	}
	if rows := accountRows(t, l); len(rows) != 1 {
		t.Errorf("owl account list reports %d accounts, want the one that was added", len(rows))
	}
}

func TestS6AccountAddRefusesAnEmptyToken(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)

	res := runOwlStdin(t, l, "", "account", "add", "work", "--token-stdin")

	if res.code == 0 {
		t.Fatalf("adding an account with no token exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "token") {
		t.Errorf("stderr does not say a token is needed:\n%s", res.stderr)
	}
	if rows := accountRows(t, l); len(rows) != 0 {
		t.Errorf("owl account list reports %+v, want no account", rows)
	}
	if _, err := os.Stat(accountDir(l, "work")); err == nil {
		t.Errorf("a configuration directory was left at %s for an account that was refused", accountDir(l, "work"))
	}
}

func TestS7AccountAddRefusesADriverOwlDoesNotHave(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)

	res := runOwlStdin(t, l, testToken, "account", "add", "work", "--driver", "codex", "--token-stdin")

	if res.code == 0 {
		t.Fatalf("adding an account on an unknown driver exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "claude-code") {
		t.Errorf("stderr does not name the drivers Owl has:\n%s", res.stderr)
	}
	if rows := accountRows(t, l); len(rows) != 0 {
		t.Errorf("owl account list reports %+v, want no account", rows)
	}
}

func TestS8AccountListReportsEveryAccount(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)

	empty := mustOwl(t, l, "account", "list").stdout

	if !strings.Contains(empty, "no accounts") {
		t.Errorf("owl account list with nothing to report says %q", strings.TrimSpace(empty))
	}
	addAccount(t, l, "work", testToken)
	addAccount(t, l, "personal", testToken+"-2")
	rows := accountRows(t, l)
	if len(rows) != 2 {
		t.Fatalf("owl account list reports %d accounts, want two:\n%+v", len(rows), rows)
	}
	if rows[0].name != "work" || rows[1].name != "personal" {
		t.Errorf("owl account list reports %s then %s, want them in the order they were added",
			rows[0].name, rows[1].name)
	}
}

func TestS9ARunCarriesTheAccountsDirectoryAndToken(t *testing.T) {
	l, s := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	accountProject(t, l, "api", "apiVersion: codingowl.dev/v1\naccount: work\n")

	finishedJob(t, l)

	inv := s.invoked(t)
	if got := inv.Env["CLAUDE_CONFIG_DIR"]; got != accountDir(l, "work") {
		t.Errorf("the agent ran with CLAUDE_CONFIG_DIR=%q, want the account's own directory %q",
			got, accountDir(l, "work"))
	}
	if got := inv.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != testToken {
		t.Errorf("the agent ran with CLAUDE_CODE_OAUTH_TOKEN=%q, want the account's token", got)
	}
	if got := inv.Env["CLAUDE_CONFIG_DIR"]; strings.HasPrefix(got, filepath.Join(l.home, ".claude")) {
		t.Errorf("the agent ran in the user's own configuration at %s", got)
	}
}

func TestS10AProjectThatNamesNoAccountRefusesToStart(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	accountProject(t, l, "api", "")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start for a project with no account exited 0\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"api", "account"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not name %q:\n%s", want, res.stderr)
		}
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job left pending", got)
	}
	if !strings.Contains(out, "runs: none") {
		t.Errorf("a run was started for a job that could not run:\n%s", out)
	}
}

func TestS11AProjectNamingAnAccountThatIsNotThereRefusesToStart(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	accountProject(t, l, "api", "apiVersion: codingowl.dev/v1\naccount: missing\n")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start for an account that is not there exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "missing") {
		t.Errorf("stderr does not name the account the project asked for:\n%s", res.stderr)
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job left pending", got)
	}
	if !strings.Contains(out, "runs: none") {
		t.Errorf("a run was started for a job that could not run:\n%s", out)
	}
}

func TestS12TheAccountAJobRanOnIsReportedWithIt(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	accountProject(t, l, "api", "apiVersion: codingowl.dev/v1\naccount: work\n")

	out, _ := finishedJob(t, l)

	if got := line(t, out, "account"); got != "work" {
		t.Errorf("owl jobs show reports account %q, want work", got)
	}
}

func TestS13AccountRemoveTakesTheAccountAndItsCredentialAway(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)

	res := mustOwl(t, l, "account", "remove", "work")

	if rows := accountRows(t, l); len(rows) != 0 {
		t.Errorf("owl account list still reports %+v", rows)
	}
	if _, ok := credentials(t, l)["account/work"]; ok {
		t.Error("the credential store still holds the account's token")
	}
	dir := accountDir(l, "work")
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the account's configuration directory went with it: %v", err)
	}
	if !strings.Contains(res.stdout, dir) {
		t.Errorf("owl account remove does not say where the directory it left behind is:\n%s", res.stdout)
	}
}

func TestS14AccountRemoveRefusesWhileAJobReferencesIt(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	accountProject(t, l, "api", "apiVersion: codingowl.dev/v1\naccount: work\n")
	finishedJob(t, l)

	res := runOwl(t, l, "account", "remove", "work")

	if res.code == 0 {
		t.Fatalf("removing an account a job references exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "1 job") {
		t.Errorf("stderr does not say how many jobs reference it:\n%s", res.stderr)
	}
	if rows := accountRows(t, l); len(rows) != 1 {
		t.Errorf("owl account list reports %+v, want the account still there", rows)
	}
}

func TestS15AccountRemoveRefusesAnAccountThatIsNotThere(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)

	res := runOwl(t, l, "account", "remove", "nothing")

	if res.code == 0 {
		t.Fatalf("removing an account that is not there exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "nothing") {
		t.Errorf("stderr does not name the account:\n%s", res.stderr)
	}
}

func TestS16AnAccountRecordsWhetherFailoverIsAllowed(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)

	addAccount(t, l, "work", testToken, "--failover")
	addAccount(t, l, "personal", testToken+"-2")

	rows := accountRows(t, l)
	if len(rows) != 2 {
		t.Fatalf("owl account list reports %d accounts, want two:\n%+v", len(rows), rows)
	}
	if rows[0].failover != "yes" {
		t.Errorf("owl account list reports failover %q for the account added with --failover, want yes", rows[0].failover)
	}
	if rows[1].failover != "no" {
		t.Errorf("owl account list reports failover %q for an account added without it, want no", rows[1].failover)
	}
}
