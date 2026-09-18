package behavior_test

// Behavior tests for issue #121. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-121.md. They read the repository rather than driving
// the daemon - the subject is documentation - except S4, which asks the built
// owl binary what commands and flags it has and holds the pages to that.

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

const (
	gettingStartedPage = "docs/guide/getting-started.md"
	jobsPage           = "docs/guide/jobs.md"
	accountsPage       = "docs/guide/projects-and-accounts.md"
	configPath         = "website/src/content.config.ts"
	loaderPath         = "website/src/loaders/repo.ts"
)

// guidePages are the three pages this issue adds, in reading order.
var guidePages = []string{gettingStartedPage, jobsPage, accountsPage}

// repoText reads a file by its repository-relative path.
func repoText(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(b)
}

// fencedLines is every line inside a fenced code block, which is where a page
// shows a command rather than talks about one.
func fencedLines(doc string) []string {
	var out []string
	fenced := false
	for _, ln := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			out = append(out, ln)
		}
	}
	return out
}

func TestS1GettingStartedIsTheSixStepHappyPath(t *testing.T) {
	doc := repoText(t, gettingStartedPage)
	if !strings.HasPrefix(doc, "# Getting started\n") {
		t.Fatalf("%s does not open with `# Getting started`", gettingStartedPage)
	}
	steps := []struct{ what, shows string }{
		{"add an Account", "owl account add"},
		{"register a Project", "owl project add"},
		{"tell Owl how to verify the work", ".coding-owl.yaml"},
		{"queue a Job", "owl add "},
		{"let it run, or make it", "owl start"},
		{"review what it did", "owl status"},
	}
	// One `##` section per step, in this order, each showing its own command
	// rather than leaving it to a section further down.
	sections := docSections(doc)
	if len(sections) < len(steps) {
		t.Fatalf("%s has %d `##` sections; the six steps are one each", gettingStartedPage, len(sections))
	}
	for i, s := range steps {
		if !strings.Contains(strings.Join(fencedLines(sections[i]), "\n"), s.shows) {
			t.Errorf("%s does not show %q in its %s section; the step that would %s is where it belongs",
				gettingStartedPage, s.shows, ordinal(i+1), s.what)
		}
	}
}

// docSections splits a page into its `##` sections in order, dropping whatever
// stands before the first one.
func docSections(doc string) []string {
	parts := strings.Split(doc, "\n## ")
	if len(parts) < 2 {
		return nil
	}
	return parts[1:]
}

// ordinal names a section the way a sentence does.
func ordinal(n int) string {
	names := []string{"first", "second", "third", "fourth", "fifth", "sixth"}
	if n >= 1 && n <= len(names) {
		return names[n-1]
	}
	return strconv.Itoa(n) + "th"
}

// docEntry is one entry of the website's docs collection.
type docEntry struct {
	id, source, title, description string
	order                          int
}

// docObject matches one object literal in content.config.ts that carries an
// id, which is what a docs collection entry is. Entries have no nested braces.
var docObject = regexp.MustCompile(`(?s)\{[^{}]*\bid:\s*'[^']*'[^{}]*\}`)

// docField reads one single-quoted field out of such an object.
func docField(object, name string) string {
	m := regexp.MustCompile(name + `:\s*'([^']*)'`).FindStringSubmatch(object)
	if m == nil {
		return ""
	}
	return m[1]
}

// docCollection is what website/src/content.config.ts registers as docs pages.
func docCollection(t *testing.T) map[string]docEntry {
	t.Helper()
	out := map[string]docEntry{}
	for _, object := range docObject.FindAllString(repoText(t, configPath), -1) {
		e := docEntry{
			id:          docField(object, "id"),
			source:      docField(object, "path"),
			title:       docField(object, "title"),
			description: docField(object, "description"),
		}
		if m := regexp.MustCompile(`order:\s*(\d+)`).FindStringSubmatch(object); m != nil {
			e.order, _ = strconv.Atoi(m[1])
		}
		out[e.id] = e
	}
	return out
}

func TestS2NewPagesArePublishedWithUrlsOfTheirOwn(t *testing.T) {
	entries := docCollection(t)
	guide, ok := entries["guide"]
	if !ok {
		t.Fatalf("%s no longer registers the guide page", configPath)
	}
	orders := map[int]string{}
	for _, want := range []string{"getting-started", "jobs", "projects-and-accounts"} {
		e, ok := entries[want]
		if !ok {
			t.Errorf("%s does not register a docs page %q", configPath, want)
			continue
		}
		if !strings.HasPrefix(e.source, "docs/guide/") {
			t.Errorf("docs page %q reads %q; the new pages live under docs/guide/", want, e.source)
		}
		if _, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(e.source))); err != nil {
			t.Errorf("docs page %q reads %q, which is not in the repository: %v", want, e.source, err)
		}
		if e.title == "" || e.description == "" {
			t.Errorf("docs page %q has title %q and description %q; both are needed", want, e.title, e.description)
		}
		if e.order >= guide.order {
			t.Errorf("docs page %q has order %d, at or behind the guide's %d; the new pages come first",
				want, e.order, guide.order)
		}
		if other, clash := orders[e.order]; clash {
			t.Errorf("docs pages %q and %q share order %d", other, want, e.order)
		}
		orders[e.order] = want
	}
}

func TestS3ManualCoversTheEverydayCommands(t *testing.T) {
	jobs := repoText(t, jobsPage)
	for _, want := range []string{
		"owl add", "owl queue list", "owl queue reorder", "owl queue remove",
		"owl start", "owl pause", "owl resume", "owl logs", "owl status",
		"owl jobs show", "owl jobs accept", "owl jobs drop", "owl jobs extend",
	} {
		if !strings.Contains(jobs, want) {
			t.Errorf("%s never shows `%s`", jobsPage, want)
		}
	}
	if !regexp.MustCompile(`owl logs \S+ -f`).MatchString(jobs) {
		t.Errorf("%s never shows following a Run with `owl logs <run> -f`", jobsPage)
	}
	accounts := repoText(t, accountsPage)
	for _, want := range []string{
		"owl project add", "owl project list", "owl project show",
		"owl project rename", "owl project move", "owl project remove",
		"owl account add", "owl account list", "owl account remove",
		"owl account exec",
		"owl account instructions show", "owl account instructions set",
		"owl account instructions edit",
	} {
		if !strings.Contains(accounts, want) {
			t.Errorf("%s never shows `%s`", accountsPage, want)
		}
	}
}

// owlNode is one command of the owl tree: the subcommands it has, the flags
// its own --help names and whether each takes a value, and how many arguments
// its usage line says it needs.
type owlNode struct {
	children map[string]*owlNode
	// flags maps every flag the command has to whether a value follows it, so
	// that the value is not counted as an argument.
	flags map[string]bool
	// needs is how many arguments the usage line shows as required.
	needs int
}

// helpSubcommands is what the "Available Commands:" block of a --help lists.
func helpSubcommands(help string) []string {
	var out []string
	listing := false
	for _, ln := range strings.Split(help, "\n") {
		if strings.HasSuffix(strings.TrimSpace(ln), "Commands:") {
			listing = true
			continue
		}
		if strings.TrimSpace(ln) == "" {
			listing = false
			continue
		}
		if !listing {
			continue
		}
		if m := regexp.MustCompile(`^\s{2}([a-z][a-z-]*)\s`).FindStringSubmatch(ln); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// helpFlagLine is one line of a Flags block: an optional short form, the long
// one, and the type of the value it takes, which a boolean flag has none of.
var helpFlagLine = regexp.MustCompile(`^\s+(?:(-[a-zA-Z]), )?(--[a-z][a-z0-9-]*)( [a-zA-Z0-9]+)?\s{2,}`)

// helpFlags is every flag a --help declares, short forms included, mapped to
// whether a value follows it. Only the Flags blocks are read: a command's
// prose mentions flags too, and prose is not what decides whether one exists.
func helpFlags(help string) map[string]bool {
	out := map[string]bool{}
	listing := false
	for _, ln := range strings.Split(help, "\n") {
		if strings.HasSuffix(strings.TrimSpace(ln), "Flags:") {
			listing = true
			continue
		}
		if strings.TrimSpace(ln) == "" {
			listing = false
			continue
		}
		if !listing {
			continue
		}
		if m := helpFlagLine.FindStringSubmatch(ln); m != nil {
			takesValue := strings.TrimSpace(m[3]) != ""
			if m[1] != "" {
				out[m[1]] = takesValue
			}
			out[m[2]] = takesValue
		}
	}
	return out
}

// usagePlaceholder is one required argument of a usage line. Optional ones are
// written in square brackets and are not counted.
var usagePlaceholder = regexp.MustCompile(`<[a-z][a-z-]*>`)

// helpNeeds is how many arguments a command's usage line shows as required.
// Counting stops at a bare `--`, because what follows it is another tool's
// command rather than this one's arguments.
func helpNeeds(help string) int {
	i := strings.Index(help, "Usage:\n")
	if i < 0 {
		return 0
	}
	usage := help[i+len("Usage:\n"):]
	if j := strings.Index(usage, "\n"); j >= 0 {
		usage = usage[:j]
	}
	if j := strings.Index(usage, " -- "); j >= 0 {
		usage = usage[:j]
	}
	return len(usagePlaceholder.FindAllString(usage, -1))
}

// owlHelp is what the built binary prints for `owl <args...> --help`.
func owlHelp(t *testing.T, args []string) string {
	t.Helper()
	cmd := exec.Command(owlBin, append(append([]string{}, args...), "--help")...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("owl %s --help: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// owlTree reads the whole command tree out of the binary's own help. The shell
// completion commands are left out: nothing documents them and walking them
// costs a process each.
func owlTree(t *testing.T, args []string) *owlNode {
	t.Helper()
	help := owlHelp(t, args)
	node := &owlNode{children: map[string]*owlNode{}, flags: helpFlags(help), needs: helpNeeds(help)}
	for _, name := range helpSubcommands(help) {
		if name == "help" || name == "completion" {
			continue
		}
		node.children[name] = owlTree(t, append(append([]string{}, args...), name))
	}
	return node
}

// docWord is one word of a documented invocation, and whether it was quoted -
// a quoted word is a prompt or a message, never a subcommand or a flag.
type docWord struct {
	text   string
	quoted bool
}

// docWords splits an invocation the way a shell would, keeping quoted words
// whole.
func docWords(s string) []docWord {
	var out []docWord
	var cur strings.Builder
	var quote rune
	started, quoted := false, false
	flush := func() {
		if started {
			out = append(out, docWord{cur.String(), quoted})
			cur.Reset()
			started, quoted = false, false
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			started, quoted = true, true
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	flush()
	return out
}

// inlineSpan is a Markdown inline code span.
var inlineSpan = regexp.MustCompile("`([^`\n]+)`")

// docInvocation is one owl command a page shows, and whether it is shown to be
// run. A line in a fenced block is: it has to be complete, arguments included.
// An inline span names a command mid-sentence and needs no arguments.
type docInvocation struct {
	raw      string
	runnable bool
}

// docInvocations is every owl command a page shows: each line of a fenced
// block that runs one, and each inline span that names one.
func docInvocations(doc string) []docInvocation {
	var out []docInvocation
	for _, ln := range fencedLines(doc) {
		if s := strings.TrimSpace(ln); s == "owl" || strings.HasPrefix(s, "owl ") {
			out = append(out, docInvocation{s, true})
		}
	}
	for _, m := range inlineSpan.FindAllStringSubmatch(doc, -1) {
		if s := strings.TrimSpace(m[1]); s == "owl" || strings.HasPrefix(s, "owl ") {
			out = append(out, docInvocation{s, false})
		}
	}
	return out
}

// subcommandName is what a word has to look like to be a subcommand somebody
// meant to type. A placeholder, a path, a flag or a number is the end of the
// command path rather than a command that is missing.
var subcommandName = regexp.MustCompile(`^[a-z][a-z-]*$`)

// checkInvocation walks one documented invocation against the real tree.
func checkInvocation(t *testing.T, root *owlNode, page string, shown docInvocation) {
	t.Helper()
	raw := shown.raw
	words := docWords(raw)
	// Everything after a bare `--` belongs to another tool, and a trailing
	// comment is prose.
	for i, w := range words {
		if !w.quoted && (w.text == "--" || strings.HasPrefix(w.text, "#")) {
			words = words[:i]
			break
		}
	}
	if len(words) == 0 || words[0].text != "owl" {
		return
	}
	node, said := root, []string{"owl"}
	i := 1
	for ; i < len(words); i++ {
		w := words[i]
		if w.quoted {
			break
		}
		child, ok := node.children[w.text]
		if !ok {
			break
		}
		node, said = child, append(said, w.text)
	}
	if i < len(words) && len(node.children) > 0 {
		if w := words[i]; !w.quoted && subcommandName.MatchString(w.text) {
			t.Errorf("%s shows `%s`, but `%s` has no %s subcommand", page, raw, strings.Join(said, " "), w.text)
			return
		}
	}
	// The arguments are what is left once the flags and their values are taken
	// out, so that `--project my-app` is not read as an argument.
	args := 0
	for j := i; j < len(words); j++ {
		w := words[j]
		if w.quoted || !strings.HasPrefix(w.text, "-") || w.text == "-" {
			args++
			continue
		}
		name, _, hadValue := strings.Cut(w.text, "=")
		if takesValue, ok := node.flags[name]; !ok {
			t.Errorf("%s shows `%s`, but `%s` has no %s flag", page, raw, strings.Join(said, " "), name)
		} else if takesValue && !hadValue {
			j++
		}
	}
	// Only a command shown in a fenced block is one a reader is meant to type,
	// so only that one is held to the arguments it needs. This is what catches
	// the `owl logs -f` the README's quick start carried: a real command with
	// a real flag, and no Run to print.
	if shown.runnable && args < node.needs {
		t.Errorf("%s shows `%s`, but `%s` needs %d argument(s) and is given %d",
			page, raw, strings.Join(said, " "), node.needs, args)
	}
}

func TestS4DocumentedCommandsAreCommandsTheBinaryHas(t *testing.T) {
	root := owlTree(t, nil)
	for _, page := range guidePages {
		doc := repoText(t, page)
		shown := docInvocations(doc)
		if len(shown) == 0 {
			t.Errorf("%s shows no owl command at all", page)
		}
		for _, one := range shown {
			checkInvocation(t, root, page, one)
		}
	}
}

// mdLink is a Markdown link target.
var mdLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// publishedFiles are the keys of PUBLISHED in the website's repo loader: the
// repository files that have a page of their own on the site.
func publishedFiles(t *testing.T) map[string]bool {
	t.Helper()
	src := repoText(t, loaderPath)
	start := strings.Index(src, "const PUBLISHED")
	if start < 0 {
		t.Fatalf("%s no longer declares PUBLISHED", loaderPath)
	}
	block := src[start:]
	if end := strings.Index(block, "};"); end >= 0 {
		block = block[:end]
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([^']+)':`).FindAllStringSubmatch(block, -1) {
		out[m[1]] = true
	}
	return out
}

// repoLinks resolves a page's relative Markdown links against the directory
// the page is in, which is the form GitHub reads and the form the loader has
// to resolve too.
func repoLinks(doc, page string) []string {
	var out []string
	for _, m := range mdLink.FindAllStringSubmatch(doc, -1) {
		target := m[1]
		if strings.HasPrefix(target, "#") || strings.HasPrefix(target, "/") ||
			regexp.MustCompile(`^[a-z]+:`).MatchString(target) {
			continue
		}
		target, _, _ = strings.Cut(target, "#")
		if target == "" {
			continue
		}
		out = append(out, path.Clean(path.Join(path.Dir(page), target)))
	}
	return out
}

func TestS5PagesLinkToPublishedPages(t *testing.T) {
	published := publishedFiles(t)
	hasAPage := map[string]bool{"README.md": true}
	for _, page := range guidePages {
		hasAPage[page] = true
	}
	for _, page := range guidePages {
		for _, target := range repoLinks(repoText(t, page), page) {
			if _, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(target))); err != nil {
				t.Errorf("%s links to %s, which is not in the repository: %v", page, target, err)
				continue
			}
			if hasAPage[target] && !published[target] {
				t.Errorf("%s links to %s, which has a page of its own but is not in PUBLISHED in %s, "+
					"so the site sends the reader to GitHub instead", page, target, loaderPath)
			}
		}
	}
}

// numberedStep is a line of an ordered list, which is how the quick start
// walks its six steps.
var numberedStep = regexp.MustCompile(`(?m)^\d+\. `)

func TestS6GuideNoLongerCarriesTheQuickStart(t *testing.T) {
	readme := repoText(t, "README.md")
	const heading = "\n## Quick start\n"
	if i := strings.Index(readme, heading); i >= 0 {
		section := readme[i+len(heading):]
		if j := strings.Index(section, "\n## "); j >= 0 {
			section = section[:j]
		}
		if numberedStep.MatchString(section) {
			t.Errorf("README.md still walks the quick start in numbered steps; they live in %s now",
				gettingStartedPage)
		}
	}
	published := publishedFiles(t)
	for _, page := range guidePages {
		if !strings.Contains(readme, "("+page+")") {
			t.Errorf("README.md does not link to %s", page)
		}
		if !published[page] {
			t.Errorf("%s is not in PUBLISHED in %s, so the guide page links to it on GitHub rather than on the site",
				page, loaderPath)
		}
	}
	if !published["README.md"] {
		t.Errorf("README.md is not in PUBLISHED in %s, so a new page linking back to the guide lands on GitHub",
			loaderPath)
	}
}
