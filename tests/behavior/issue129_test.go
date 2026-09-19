package behavior_test

// Behavior tests for issue #129. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-129.md. The deliverable is a pair of generated files
// and the wiring that keeps them current, so the scenarios read what is on
// disk, run the generator, and read the Makefile, the CI workflow and the
// docs that promise the rest.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/config"
)

// The two published schemas, relative to the repository root, and the URL each
// one is served from. website/public is copied verbatim into the site, so the
// path and the URL are the same thing said twice (issue #129).
const (
	projectSchemaPath = "website/public/schema/project.json"
	daemonSchemaPath  = "website/public/schema/daemon.json"
	projectSchemaURL  = "https://codingowl.dev/schema/project.json"
	daemonSchemaURL   = "https://codingowl.dev/schema/daemon.json"
)

// draft07 is the dialect the schemas declare. It is what the YAML language
// servers behind editor intellisense support best.
const draft07 = "http://json-schema.org/draft-07/schema#"

// schemaGenerator is the package `make generate` runs to write the schemas.
const schemaGenerator = "./internal/config/schemagen"

// object is a decoded JSON object, which is all a schema is.
type object map[string]any

// schema reads one of the published schemas. It fails the scenario outright
// when it is not there or is not JSON: every other assertion about it would be
// vacuously true.
func schema(t *testing.T, rel string) object {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoDir, rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	var out object
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s is not JSON: %v", rel, err)
	}
	return out
}

// at walks a path of property names through a schema, stepping over the
// `properties` and `additionalProperties` keywords that separate them, and
// returns the subschema it lands on. A missing step fails the scenario naming
// the path, because "no such property" and "the wrong shape" want different
// messages.
//
// A step of `*` is the schema of a map's values, which is where
// `additionalProperties` holds a subschema rather than false.
func at(t *testing.T, s object, path ...string) object {
	t.Helper()
	cur := s
	for i, step := range path {
		where := strings.Join(path[:i+1], ".")
		if step == "*" {
			next, ok := cur["additionalProperties"].(map[string]any)
			if !ok {
				t.Fatalf("%s: additionalProperties is not a schema", where)
			}
			cur = next
			continue
		}
		if items, ok := cur["items"].(map[string]any); ok {
			cur = items
		}
		props, ok := cur["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: there are no properties to look in", where)
		}
		next, ok := props[step].(map[string]any)
		if !ok {
			t.Fatalf("%s: no such property", where)
		}
		cur = next
	}
	return cur
}

// propertyNames are the property names of a subschema, sorted, which is how a
// scenario says what a block may hold.
func propertyNames(t *testing.T, s object) []string {
	t.Helper()
	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatalf("this schema declares no properties: %v", s)
	}
	out := make([]string, 0, len(props))
	for name := range props {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// strings reads a JSON array of strings, for `required` and `enum`.
func stringsOf(t *testing.T, v any, what string) []string {
	t.Helper()
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("%s is not a list: %v", what, v)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("%s holds %v, which is not a string", what, item)
		}
		out = append(out, s)
	}
	return out
}

// generate runs the schema generator with the repository as its working
// directory and fails the scenario on a non-zero exit.
func generate(t *testing.T) {
	t.Helper()
	cmd := exec.Command("go", "run", schemaGenerator)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go run %s: %v\n%s", schemaGenerator, err, out)
	}
}

// accepts reports whether an anyOf branch of that JSON type is there, which is
// how the settings read as whatever was written say they take two forms.
func accepts(t *testing.T, s object, jsonType string) bool {
	t.Helper()
	branches, ok := s["anyOf"].([]any)
	if !ok {
		return false
	}
	for _, b := range branches {
		branch, ok := b.(map[string]any)
		if ok && branch["type"] == jsonType {
			return true
		}
	}
	return false
}

func TestS1SchemasArePublishedWithTheSite(t *testing.T) {
	for rel, want := range map[string]string{
		projectSchemaPath: projectSchemaURL,
		daemonSchemaPath:  daemonSchemaURL,
	} {
		s := schema(t, rel)

		if s["$schema"] != draft07 {
			t.Errorf("%s declares $schema %v, want %q", rel, s["$schema"], draft07)
		}
		if s["$id"] != want {
			t.Errorf("%s declares $id %v, want %q", rel, s["$id"], want)
		}
		for _, key := range []string{"title", "description"} {
			if text, ok := s[key].(string); !ok || strings.TrimSpace(text) == "" {
				t.Errorf("%s has no %s saying which file it is for", rel, key)
			}
		}
	}
}

func TestS2CommittedSchemasAreWhatTheStructsProduce(t *testing.T) {
	before := map[string][]byte{}
	for _, rel := range []string{projectSchemaPath, daemonSchemaPath} {
		data, err := os.ReadFile(filepath.Join(repoDir, rel))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		before[rel] = data
	}

	generate(t)

	for rel, was := range before {
		now, err := os.ReadFile(filepath.Join(repoDir, rel))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		if string(now) != string(was) {
			t.Errorf("%s is stale: regenerating it changed it", rel)
		}
	}
}

func TestS3MakeGenerateRegeneratesThem(t *testing.T) {
	makefile := readFile(t, filepath.Join(repoDir, "Makefile"))
	target, ok := makeTarget(makefile, "generate")
	if !ok {
		t.Fatalf("the Makefile has no generate target:\n%s", makefile)
	}
	if !strings.Contains(target, schemaGenerator) {
		t.Errorf("make generate does not run the schema generator:\n%s", target)
	}
	if !strings.Contains(target, "buf generate") {
		t.Errorf("make generate no longer runs buf generate:\n%s", target)
	}

	// The generator writes the files rather than only checking them, so it
	// puts back what is not there.
	committed := map[string][]byte{}
	for _, rel := range []string{projectSchemaPath, daemonSchemaPath} {
		path := filepath.Join(repoDir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		committed[rel] = data
		if err := os.Remove(path); err != nil {
			t.Fatalf("removing %s: %v", rel, err)
		}
		t.Cleanup(func() { _ = os.WriteFile(path, data, 0o644) })
	}

	generate(t)

	for rel, was := range committed {
		now, err := os.ReadFile(filepath.Join(repoDir, rel))
		if err != nil {
			t.Fatalf("%s was not written back: %v", rel, err)
		}
		if string(now) != string(was) {
			t.Errorf("%s came back different from what is committed", rel)
		}
	}
}

// makeTarget returns the recipe of a Makefile target: its lines up to the next
// line that starts in the first column.
func makeTarget(makefile, name string) (string, bool) {
	lines := strings.Split(makefile, "\n")
	for i, ln := range lines {
		if !strings.HasPrefix(ln, name+":") {
			continue
		}
		var recipe []string
		for _, next := range lines[i+1:] {
			if next != "" && !strings.HasPrefix(next, "\t") && !strings.HasPrefix(next, " ") {
				break
			}
			recipe = append(recipe, next)
		}
		return strings.Join(recipe, "\n"), true
	}
	return "", false
}

func TestS4APIVersionIsRequiredAndPinned(t *testing.T) {
	for _, rel := range []string{projectSchemaPath, daemonSchemaPath} {
		s := schema(t, rel)

		if got := at(t, s, "apiVersion")["const"]; got != config.APIVersion {
			t.Errorf("%s: apiVersion const is %v, want %q", rel, got, config.APIVersion)
		}
		if required := stringsOf(t, s["required"], rel+" required"); !slices.Contains(required, "apiVersion") {
			t.Errorf("%s: apiVersion is not required; required is %v", rel, required)
		}
	}
}

func TestS5AKeyTheDecoderWouldRefuseIsInvalid(t *testing.T) {
	for _, rel := range []string{projectSchemaPath, daemonSchemaPath} {
		data := readFile(t, filepath.Join(repoDir, rel))
		var root any
		if err := json.Unmarshal([]byte(data), &root); err != nil {
			t.Fatalf("%s is not JSON: %v", rel, err)
		}

		open := openObjects(root, "")

		if len(open) > 0 {
			t.Errorf("%s: these objects do not say additionalProperties: false, "+
				"so the schema accepts keys the decoder refuses: %v", rel, open)
		}
	}
}

// openObjects returns the paths of every subschema that declares properties
// without closing the object. A subschema whose additionalProperties is itself
// a schema is a map, which is closed by that schema rather than by false.
func openObjects(node any, path string) []string {
	obj, ok := node.(map[string]any)
	if !ok {
		var out []string
		if list, ok := node.([]any); ok {
			for i, item := range list {
				out = append(out, openObjects(item, path)...)
				_ = i
			}
		}
		return out
	}
	var out []string
	_, hasProps := obj["properties"]
	if hasProps {
		switch obj["additionalProperties"].(type) {
		case bool:
			if obj["additionalProperties"] != false {
				out = append(out, orRoot(path))
			}
		case map[string]any:
			// A map: its values are described, which closes it.
		default:
			out = append(out, orRoot(path))
		}
	}
	for key, child := range obj {
		if key == "const" || key == "enum" || key == "default" || key == "examples" {
			continue
		}
		next := key
		if path != "" {
			next = path + "." + key
		}
		out = append(out, openObjects(child, next)...)
	}
	return out
}

func orRoot(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func TestS6EachSchemaDescribesItsOwnFileKind(t *testing.T) {
	project := schema(t, projectSchemaPath)
	daemon := schema(t, daemonSchemaPath)

	wantProject := []string{
		"account", "allowedTools", "apiVersion", "branchPrefix", "budgetUSD",
		"checks", "maxParallelRuns", "phases", "setup", "skills",
		"unattendedClauses", "verification",
	}
	if got := propertyNames(t, project); !slices.Equal(got, wantProject) {
		t.Errorf("the project schema's properties are\n%v\nwant\n%v", got, wantProject)
	}
	wantDaemon := []string{
		"accounts", "apiVersion", "claudePath", "credentialStore", "garbageCollection",
		"graceWindow", "idle", "maxParallelRuns", "phases",
	}
	if got := propertyNames(t, daemon); !slices.Equal(got, wantDaemon) {
		t.Errorf("the daemon schema's properties are\n%v\nwant\n%v", got, wantDaemon)
	}
}

func TestS7WhateverWasWrittenTakesANumberOrAString(t *testing.T) {
	project := schema(t, projectSchemaPath)
	daemon := schema(t, daemonSchemaPath)

	for what, s := range map[string]object{
		"the project schema's maxParallelRuns": at(t, project, "maxParallelRuns"),
		"the daemon schema's maxParallelRuns":  at(t, daemon, "maxParallelRuns"),
		"an account's maxParallel":             at(t, daemon, "accounts", "*", "maxParallel"),
	} {
		if !accepts(t, s, "integer") {
			t.Errorf("%s does not accept an integer: %v", what, s)
		}
		if !accepts(t, s, "string") {
			t.Errorf("%s does not accept a string: %v", what, s)
		}
		if s["type"] != nil {
			t.Errorf("%s pins a single type %v, which is not what the parser reads", what, s["type"])
		}
	}
}

func TestS8AnAccountsLimitsNameOnlyTheCeilingsOwlKeeps(t *testing.T) {
	daemon := schema(t, daemonSchemaPath)

	limits := at(t, daemon, "accounts", "*", "limits")

	want := []string{"fiveHourMax", "weeklyMax"}
	if got := propertyNames(t, limits); !slices.Equal(got, want) {
		t.Errorf("an account's limits are %v, want %v", got, want)
	}
	if limits["additionalProperties"] != false {
		t.Errorf("an account's limits accept a ceiling Owl does not keep: %v", limits["additionalProperties"])
	}
}

func TestS9TheClosedSetsOwlKeepsAreEnums(t *testing.T) {
	project := schema(t, projectSchemaPath)
	daemon := schema(t, daemonSchemaPath)

	expect := at(t, project, "checks", "expect")
	if got := stringsOf(t, expect["enum"], "a check's expect"); !slices.Equal(got, []string{config.ExpectEmptyOutput}) {
		t.Errorf("a check's expect is %v, want [%s]", got, config.ExpectEmptyOutput)
	}

	for rel, s := range map[string]object{projectSchemaPath: project, daemonSchemaPath: daemon} {
		want := []string{config.PhaseExecute, config.PhasePlan}
		if got := propertyNames(t, at(t, s, "phases")); !slices.Equal(got, want) {
			t.Errorf("%s: phases are %v, want %v", rel, got, want)
		}
	}

	store := at(t, daemon, "credentialStore")
	if got := stringsOf(t, store["enum"], "credentialStore"); !slices.Equal(got, []string{"file", "keychain"}) {
		t.Errorf("credentialStore is %v, want [file keychain]", got)
	}
}

func TestS10ADurationIsDescribedAsOne(t *testing.T) {
	project := schema(t, projectSchemaPath)
	daemon := schema(t, daemonSchemaPath)

	durations := map[string]object{
		"graceWindow":                at(t, daemon, "graceWindow"),
		"idle.after":                 at(t, daemon, "idle", "after"),
		"garbageCollection.interval": at(t, daemon, "garbageCollection", "interval"),
		"a phase's timeout":          at(t, project, "phases", "plan", "timeout"),
		"a phase's stall":            at(t, project, "phases", "execute", "stall"),
	}
	for what, s := range durations {
		if s["type"] != "string" {
			t.Errorf("%s is typed %v, want string: a duration is written as one", what, s["type"])
		}
		pattern, ok := s["pattern"].(string)
		if !ok {
			t.Errorf("%s has no pattern, so any text at all would pass", what)
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Errorf("%s has a pattern that is not a regexp: %v", what, err)
			continue
		}
		if !re.MatchString("4h") {
			t.Errorf("%s does not match 4h, which is a duration", what)
		}
		if re.MatchString("soon") {
			t.Errorf("%s matches soon, which is not a duration", what)
		}
	}
}

func TestS11TheDocumentedConfigurationSatisfiesItsSchema(t *testing.T) {
	readme := readFile(t, filepath.Join(repoDir, "README.md"))
	project, daemon := documentedConfigs(t, readme)

	if _, err := config.Parse("README.md", []byte(project)); err != nil {
		t.Fatalf("the documented project configuration is not one Owl accepts: %v", err)
	}
	if _, err := config.ParseGlobal("README.md", []byte(daemon)); err != nil {
		t.Fatalf("the documented daemon configuration is not one Owl accepts: %v", err)
	}

	for _, c := range []struct {
		what, yaml, path string
	}{
		{"the project", project, projectSchemaPath},
		{"the daemon", daemon, daemonSchemaPath},
	} {
		s := schema(t, c.path)
		for _, key := range documentedKeys(c.yaml) {
			if missing := undeclared(s, key); missing != "" {
				t.Errorf("%s configuration in README.md writes %s, which %s does not declare (%s is missing)",
					c.what, key, c.path, missing)
			}
		}
	}
}

// documentedConfigs pulls the project and daemon YAML examples out of the
// README's Configuration section. They are the two fenced yaml blocks that
// open with the apiVersion line, in that order.
func documentedConfigs(t *testing.T, readme string) (string, string) {
	t.Helper()
	fence := regexp.MustCompile("(?s)```yaml\n(.*?)```")
	var configs []string
	for _, m := range fence.FindAllStringSubmatch(readme, -1) {
		if strings.HasPrefix(strings.TrimSpace(m[1]), "apiVersion:") {
			configs = append(configs, m[1])
		}
	}
	if len(configs) < 2 {
		t.Fatalf("README.md documents %d configuration files, want the project's and the daemon's", len(configs))
	}
	return configs[0], configs[1]
}

// documentedKeys are the dotted paths of the mapping keys in a YAML document,
// read off the indentation. A list item's own keys are nested under the key
// the list belongs to, which is how the schema nests them too. Comments and
// list items that are not mappings are skipped.
func documentedKeys(doc string) []string {
	keyRE := regexp.MustCompile(`^(\s*)(?:- )?([A-Za-z_][A-Za-z0-9_]*):(?:\s|$)`)
	var out []string
	var stack []struct {
		indent int
		key    string
	}
	for _, ln := range strings.Split(doc, "\n") {
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		m := keyRE.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		indent := len(m[1])
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		path := m[2]
		if len(stack) > 0 {
			path = stack[len(stack)-1].key + "." + path
		}
		stack = append(stack, struct {
			indent int
			key    string
		}{indent, path})
		out = append(out, path)
	}
	return out
}

// undeclared walks a dotted key through a schema and returns the first step
// the schema does not declare, or an empty string when it declares them all.
// A step the schema takes as a map's key - an Account's name, a phase already
// named as a property - is stepped over rather than looked up.
func undeclared(s object, key string) string {
	cur := s
	for _, step := range strings.Split(key, ".") {
		if items, ok := cur["items"].(map[string]any); ok {
			cur = items
		}
		if props, ok := cur["properties"].(map[string]any); ok {
			if next, ok := props[step].(map[string]any); ok {
				cur = next
				continue
			}
		}
		if next, ok := cur["additionalProperties"].(map[string]any); ok {
			cur = next
			continue
		}
		return step
	}
	return ""
}

func TestS12CIRefusesAStaleSchema(t *testing.T) {
	ci := readFile(t, filepath.Join(repoDir, ".github/workflows/ci.yml"))

	if !strings.Contains(ci, "website/public/schema") {
		t.Errorf("no CI step names the published schemas:\n%s", ci)
	}
	var checked bool
	for _, ln := range strings.Split(ci, "\n") {
		if strings.Contains(ln, "website/public/schema") && strings.Contains(ln, "git diff --exit-code") {
			checked = true
		}
	}
	if !checked {
		t.Errorf("CI does not regenerate the schemas and fail on a difference, "+
			"the way it already does for the generated protobuf code:\n%s", ci)
	}
}

func TestS13DocsSayHowToPointAnEditorAtThem(t *testing.T) {
	readme := readFile(t, filepath.Join(repoDir, "README.md"))

	const directive = "# yaml-language-server: $schema="
	for _, want := range []string{directive + projectSchemaURL, directive + daemonSchemaURL} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md does not show %q", want)
		}
	}
}
