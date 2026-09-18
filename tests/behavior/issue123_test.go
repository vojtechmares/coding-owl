package behavior_test

// Behavior tests for issue #123. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-123.md. The deliverable is a docs page, so the
// scenarios read it off disk and measure what it claims against the thing it
// describes: the Go source of truth in internal/chat, the built owl binary,
// and the website's own registry of published pages.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/chat"
)

// providersPage is the page issue #123 asks for, relative to the repository
// root. It is the one string every scenario here turns on.
const providersPage = "docs/guide/chat-providers.md"

// page is the page's text, and fails the scenario outright when it is not
// there: every other assertion about it would be vacuously true.
func page(t *testing.T) string {
	t.Helper()
	body := readFile(t, filepath.Join(repoDir, providersPage))
	if body == "" {
		t.Fatalf("%s is not in the repository", providersPage)
	}
	return body
}

// flat is text with every run of whitespace collapsed to one space, so a claim
// can be looked for without the line wrapping of wherever it was written
// deciding whether it is found.
func flat(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// breaks are the lines that end a sentence without a full stop: a blank line,
// a heading, a list item, a fence, a table row. Without them a claim could be
// read across a code block, or out of two headings that happen to sit side by
// side, and that is not a claim the page makes.
var breaks = regexp.MustCompile("(?m)^\\s*$|^\\s*#+ |^\\s*[-*+] |^\\s*```|^\\s*\\|")

// sentences are the sentences of some prose, flattened, for asking whether a
// claim is made rather than whether two words happen to both appear.
func sentences(text string) []string {
	var out []string
	var block []string
	flush := func() {
		if len(block) == 0 {
			return
		}
		for _, s := range strings.Split(flat(strings.Join(block, " ")), ". ") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		block = nil
	}
	for _, ln := range strings.Split(text, "\n") {
		if !breaks.MatchString(ln) {
			block = append(block, ln)
			continue
		}
		flush()
		// A heading or a list item is a sentence of its own, not part of the
		// paragraph on either side of it.
		if strings.TrimSpace(ln) != "" {
			block = append(block, strings.TrimLeft(strings.TrimSpace(ln), "#-*+| "))
			flush()
		}
	}
	flush()
	return out
}

// word is how one of those words is looked for: whole, where it is made of
// word characters, so "no" is not found inside "know" or "not". A flag or a
// command with punctuation in it is looked for as it is written.
func word(w string) *regexp.Regexp {
	w = strings.ToLower(w)
	pattern := regexp.QuoteMeta(w)
	if regexp.MustCompile(`^\w.*\w$|^\w$`).MatchString(w) {
		pattern = `\b` + pattern + `\b`
	}
	return regexp.MustCompile(pattern)
}

// claims reports whether some sentence of the page makes one claim: it carries
// every one of those words. A claim spread over two sentences is not one the
// page makes.
func claims(text string, words ...string) bool {
	res := make([]*regexp.Regexp, len(words))
	for i, w := range words {
		res[i] = word(w)
	}
	for _, s := range sentences(strings.ToLower(text)) {
		all := true
		for _, re := range res {
			if !re.MatchString(s) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// addedProviders are the providers the page shows being configured, which is
// every `owl providers add <name>` written on it. The placeholder form is not
// one: it stands for whichever provider the reader has.
var addedRE = regexp.MustCompile(`owl providers add ([A-Za-z<][A-Za-z0-9<>_-]*)`)

func addedProviders(text string) []string {
	var out []string
	for _, m := range addedRE.FindAllStringSubmatch(text, -1) {
		if strings.HasPrefix(m[1], "<") {
			continue
		}
		out = append(out, m[1])
	}
	return out
}

func TestS123PageIsInTheRepositoryAndPublished(t *testing.T) {
	body := page(t)

	if !strings.HasPrefix(strings.TrimSpace(body), "# ") {
		t.Errorf("%s does not open with an H1:\n%s", providersPage, flat(body)[:min(120, len(flat(body)))])
	}

	// The docs collection is where a repository file becomes a page.
	config := readFile(t, filepath.Join(repoDir, "website", "src", "content.config.ts"))
	entryRE := regexp.MustCompile(`(?s)\{([^{}]*'` + regexp.QuoteMeta(providersPage) + `'[^{}]*)\}`)
	m := entryRE.FindStringSubmatch(config)
	if m == nil {
		t.Fatalf("website/src/content.config.ts has no docs entry for %s", providersPage)
	}
	entry := m[1]
	for _, field := range []string{"id:", "title:", "description:", "order:"} {
		if !strings.Contains(entry, field) {
			t.Errorf("the docs entry for %s has no %s\n%s", providersPage, field, entry)
		}
	}
	idM := regexp.MustCompile(`id:\s*'([^']+)'`).FindStringSubmatch(entry)
	if idM == nil {
		t.Fatalf("the docs entry for %s names no id:\n%s", providersPage, entry)
	}
	url := "/docs/" + idM[1]

	// And PUBLISHED is what makes a link to the file in the repository resolve
	// to that page rather than out to GitHub.
	loader := readFile(t, filepath.Join(repoDir, "website", "src", "loaders", "repo.ts"))
	published := regexp.MustCompile(`(?s)const PUBLISHED[^{]*\{(.*?)\n\};`).FindStringSubmatch(loader)
	if published == nil {
		t.Fatal("website/src/loaders/repo.ts has no PUBLISHED map")
	}
	want := `'` + providersPage + `': '` + url + `'`
	if !strings.Contains(flat(published[1]), want) {
		t.Errorf("PUBLISHED does not carry %s:\n%s", want, published[1])
	}

	// A page under docs/guide/ writes its links relative to itself, which is
	// how GitHub reads them too, so the rewriter has to resolve them that way
	// before looking them up. The website is not built by `make test`, so what
	// it is made of is checked on disk, as issue #9's and #18's frontend
	// scenarios do - the two things any resolution needs, rather than the name
	// this one happens to give its helper.
	if !regexp.MustCompile(`(?s)rewriteRepoLinks\(.{0,300}?file\.path`).MatchString(loader) {
		t.Error("rewriteRepoLinks is not told which file the links it rewrites are written in")
	}
	if !regexp.MustCompile(`path(?:\.posix)?\.dirname\(`).MatchString(loader) {
		t.Error("repo.ts never takes the directory of that file, so it cannot resolve a link against it")
	}
}

func TestS123PageDocumentsEveryProviderAndNoOther(t *testing.T) {
	body := page(t)

	for _, p := range chat.Providers {
		if !strings.Contains(strings.ToLower(body), p) {
			t.Errorf("%s does not mention %s, which Owl drives", providersPage, p)
		}
	}

	// Every provider the page shows being configured is one Owl really drives,
	// and every one it drives is shown.
	shown := map[string]bool{}
	for _, name := range addedProviders(body) {
		if !known(name) {
			t.Errorf("the page shows `owl providers add %s`, which is not a provider Owl drives: %v",
				name, chat.Providers)
		}
		shown[name] = true
	}
	for _, p := range chat.Providers {
		if !shown[p] {
			t.Errorf("the page never shows %s being configured", p)
		}
	}

	// OpenAI is a wire shape OpenRouter speaks, not a provider. Any sentence
	// naming it has to be about OpenRouter, or the page has invented a third
	// provider.
	// A model id such as openai/gpt-5 names a model OpenRouter carries, not a
	// provider, so it is taken out before the name is looked for.
	bare := regexp.MustCompile(`(?i)\bopenai\b`)
	ids := regexp.MustCompile(`(?i)\bopenai/`)
	for _, s := range sentences(body) {
		s = ids.ReplaceAllString(s, "a-model-of/")
		if bare.MatchString(s) && !strings.Contains(strings.ToLower(s), "openrouter") {
			t.Errorf("the page names OpenAI apart from OpenRouter, as if it were a provider: %q", s)
		}
	}
}

// known is whether a name is a provider Owl drives.
func known(name string) bool {
	for _, p := range chat.Providers {
		if p == name {
			return true
		}
	}
	return false
}

func TestS123AnthropicsModelsAreTheOnesOwlKnows(t *testing.T) {
	body := page(t)
	defaults := chat.DefaultModels[chat.Anthropic]
	if len(defaults) == 0 {
		t.Fatal("anthropic has no default models, so there is nothing for the page to list")
	}

	for _, model := range defaults {
		if !strings.Contains(body, model) {
			t.Errorf("%s does not list %s, which Owl offers for anthropic", providersPage, model)
		}
	}
	// And no model Owl does not offer, which is the drift the issue warns of.
	// The whole page, not only Anthropic's section, so a name left behind
	// anywhere on it is caught. A model id OpenRouter carries is spelt the same
	// but is qualified by the vendor it comes from, and is nothing to do with
	// what Owl knows, so those are taken out first.
	want := map[string]bool{}
	for _, model := range defaults {
		want[model] = true
	}
	ours := regexp.MustCompile(`[a-z0-9]+/claude-[a-z0-9.-]+`).ReplaceAllString(body, "a-model-of/theirs")
	for _, got := range regexp.MustCompile(`claude-[a-z0-9.-]+`).FindAllString(ours, -1) {
		if !want[got] {
			t.Errorf("the page lists %s for anthropic, which Owl does not offer: %v", got, defaults)
		}
	}
	if !claims(body, "anthropic", "--model", "no") && !claims(body, "anthropic", "--model", "without") {
		t.Errorf("%s does not say anthropic needs no --model", providersPage)
	}

	// And what the page says is what the binary does.
	l := newLayout(t)
	globalConfig(t, l, fileStore)
	daemonUp(t, l)

	added := runOwlStdin(t, l, "sk-ant-test", "providers", "add", "anthropic", "--key-stdin")

	if added.code != 0 {
		t.Fatalf("anthropic with no --model was refused, which the page says it is not:\n%s", added.stderr)
	}
	listed := mustOwl(t, l, "providers", "list").stdout
	for _, model := range defaults {
		if !strings.Contains(listed, model) {
			t.Errorf("owl providers list does not report %s:\n%s", model, listed)
		}
	}
}

func TestS123OpenRouterNeedsAModel(t *testing.T) {
	body := page(t)

	if !claims(body, "openrouter", "--model", "required") &&
		!claims(body, "openrouter", "--model", "name the models") &&
		!claims(body, "openrouter", "--model", "must") {
		t.Errorf("%s does not say --model is required for openrouter", providersPage)
	}
	// And why: what OpenRouter offers is the user's own account's business.
	if !claims(body, "openrouter", "account") {
		t.Errorf("%s does not say why openrouter needs the models named", providersPage)
	}

	l := newLayout(t)
	globalConfig(t, l, fileStore)
	daemonUp(t, l)

	res := runOwlStdin(t, l, "sk-or-test", "providers", "add", "openrouter", "--key-stdin")

	if res.code == 0 {
		t.Fatalf("openrouter with no --model was accepted, which the page says it is not:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "--model") {
		t.Errorf("stderr does not name the flag the page names:\n%s", res.stderr)
	}
}

func TestS123AKeyIsReadFromStandardInputAndNowhereElse(t *testing.T) {
	body := page(t)

	if !strings.Contains(body, "--key-stdin") {
		t.Errorf("%s does not show --key-stdin", providersPage)
	}
	if !claims(body, "key", "standard input") {
		t.Errorf("%s does not say the key is read from standard input", providersPage)
	}
	if !claims(body, "credential store") || !strings.Contains(strings.ToLower(body), "keychain") {
		t.Errorf("%s does not say where the key goes", providersPage)
	}
	if !claims(body, "database", "reference") {
		t.Errorf("%s does not say the database keeps only a reference to the key", providersPage)
	}
	for _, adr := range []string{"ADR-0019", "ADR-0022"} {
		if !strings.Contains(body, adr) {
			t.Errorf("%s does not cite %s", providersPage, adr)
		}
	}

	l := newLayout(t)
	globalConfig(t, l, fileStore)
	daemonUp(t, l)

	refused := runOwl(t, l, "providers", "add", "anthropic")

	if refused.code == 0 {
		t.Fatalf("a provider was configured with no key at all:\n%s", refused.stdout)
	}
	if !strings.Contains(refused.stderr, "--key-stdin") {
		t.Errorf("stderr does not say where a key is read from:\n%s", refused.stderr)
	}
	// And there is no second way in for the page to have described: no flag
	// takes a key as an argument, where the shell history and every process on
	// the machine would see it (ADR-0019).
	help := mustOwl(t, l, "providers", "add", "--help").stdout
	for _, flag := range regexp.MustCompile(`--[a-z][a-z-]*`).FindAllString(help, -1) {
		if strings.Contains(flag, "key") && flag != "--key-stdin" {
			t.Errorf("owl providers add offers %s, which takes a key as an argument:\n%s", flag, help)
		}
	}
}

func TestS123EverySubcommandThePageNamesIsReal(t *testing.T) {
	body := page(t)
	l := newLayout(t)

	help := mustOwl(t, l, "providers", "--help").stdout

	real := map[string]bool{}
	_, commands, ok := strings.Cut(help, "Available Commands:")
	if !ok {
		t.Fatalf("owl providers --help lists no commands:\n%s", help)
	}
	commands, _, _ = strings.Cut(commands, "\nFlags:")
	for _, ln := range strings.Split(commands, "\n") {
		if name, _, _ := strings.Cut(strings.TrimSpace(ln), " "); name != "" {
			real[name] = true
		}
	}
	for _, want := range []string{"add", "list", "remove"} {
		if !real[want] {
			t.Fatalf("owl providers has no %s subcommand, so the sheet is out of date:\n%s", want, commands)
		}
	}

	named := map[string]bool{}
	for _, m := range regexp.MustCompile(`owl providers ([a-z][a-z-]*)`).FindAllStringSubmatch(body, -1) {
		named[m[1]] = true
	}
	for name := range named {
		if !real[name] {
			t.Errorf("the page names `owl providers %s`, which the binary does not have:\n%s", name, commands)
		}
	}
	for _, want := range []string{"add", "list", "remove"} {
		if !named[want] {
			t.Errorf("%s does not name `owl providers %s`", providersPage, want)
		}
	}
}

func TestS123PageSaysWhatAProviderIsNot(t *testing.T) {
	body := page(t)
	l := newLayout(t)

	// The distinction, not the word. A page that merely says "account"
	// somewhere says nothing: what it has to say is what an Account is and
	// where one is configured, which is somewhere else entirely.
	if !claims(body, "account", "subscription", "owl account add") {
		t.Errorf("%s does not say what an Agent's Account is and where it is configured, "+
			"so it does not distinguish one from a model provider", providersPage)
	}
	if !claims(body, "driver", "coding tool", "owl account add --driver") {
		t.Errorf("%s does not say what a Driver is and where it is chosen, "+
			"so it does not distinguish one from a model provider", providersPage)
	}
	// And those are the commands the binary really has, so the distinction
	// cannot be drawn against a command nobody can run.
	accounts := mustOwl(t, l, "account", "--help").stdout
	_, commands, ok := strings.Cut(accounts, "Available Commands:")
	if !ok {
		t.Fatalf("owl account --help lists no commands:\n%s", accounts)
	}
	commands, _, _ = strings.Cut(commands, "\nFlags:")
	// The name a line starts with, rather than the word anywhere in it: a
	// subcommand's own summary could say "added" and prove nothing.
	var hasAdd bool
	for _, ln := range strings.Split(commands, "\n") {
		if name, _, _ := strings.Cut(strings.TrimSpace(ln), " "); name == "add" {
			hasAdd = true
		}
	}
	if !hasAdd {
		t.Errorf("the page says an Account is configured with `owl account add`, "+
			"which the binary does not have:\n%s", commands)
	}
	if !strings.Contains(mustOwl(t, l, "account", "add", "--help").stdout, "--driver") {
		t.Errorf("the page says a Driver is chosen with `owl account add --driver`, " +
			"which takes no such flag")
	}

	if !strings.Contains(body, "CONTEXT.md") {
		t.Errorf("%s does not link to CONTEXT.md, where Account and Driver are defined", providersPage)
	}
	// The chat's own framing: it reads what Owl knows, and never acts.
	if !claims(body, "never acts") && !claims(body, "never act") {
		t.Errorf("%s does not say the chat never acts (ADR-0022)", providersPage)
	}
}

func TestS123NothingOnThePageDangles(t *testing.T) {
	body := page(t)
	dir := filepath.Dir(filepath.Join(repoDir, providersPage))

	links := regexp.MustCompile(`\]\(((?:[a-z]+:)?[^)\s]+)\)`).FindAllStringSubmatch(body, -1)
	if len(links) == 0 {
		t.Fatalf("%s carries no links at all", providersPage)
	}
	var relative int
	for _, m := range links {
		target := m[1]
		if strings.Contains(target, ":") || strings.HasPrefix(target, "#") {
			continue
		}
		relative++
		target, _, _ = strings.Cut(target, "#")
		if target == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
			t.Errorf("the link to %q leads nowhere: %v", m[1], err)
		}
	}
	if relative == 0 {
		t.Errorf("%s links to nothing in the repository", providersPage)
	}
}

func TestS123PageAndTheCLIHelpSayTheSameThing(t *testing.T) {
	body := flat(page(t))
	l := newLayout(t)

	for _, args := range [][]string{{"providers", "--help"}, {"providers", "add", "--help"}} {
		help := mustOwl(t, l, args...).stdout
		long, _, ok := strings.Cut(help, "\nUsage:")
		if !ok {
			t.Fatalf("owl %v prints no help:\n%s", args, help)
		}
		for _, claim := range helpClaims(long) {
			if !strings.Contains(body, claim) {
				t.Errorf("%s does not carry what `owl %s` says:\n  %s",
					providersPage, strings.Join(args, " "), claim)
			}
		}
	}
}

// helpClaims are the sentences of a command's help text, which the page is to
// carry rather than explain a second time in words of its own (issue #123).
// Indented example commands are left out: the page shows them as fenced code,
// which is the same text but not the same sentence.
func helpClaims(long string) []string {
	var out []string
	for _, para := range strings.Split(strings.TrimSpace(long), "\n\n") {
		if strings.HasPrefix(para, "    ") || strings.HasPrefix(para, "\t") {
			continue
		}
		for _, s := range strings.Split(flat(para), ". ") {
			s = strings.TrimSuffix(strings.TrimSpace(s), ".")
			if len(s) > 30 {
				out = append(out, s)
			}
		}
	}
	return out
}

func TestS123TheRepositoryPointsAtThePage(t *testing.T) {
	readme := readFile(t, filepath.Join(repoDir, "README.md"))
	if readme == "" {
		t.Fatal("README.md is not there")
	}

	_, desktop, ok := strings.Cut(readme, "\n## Desktop app\n")
	if !ok {
		t.Fatal("README.md has no Desktop app section")
	}
	desktop, _, _ = strings.Cut(desktop, "\n## ")

	if !strings.Contains(desktop, providersPage) {
		t.Errorf("README.md's Desktop app section does not link to %s:\n%s", providersPage, desktop)
	}
}
