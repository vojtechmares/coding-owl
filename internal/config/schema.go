package config

// The JSON schemas of the two configuration file kinds, generated from the
// same on-disk shapes the decoder reads into (issue #129).
//
// They are generated rather than written by hand because a hand-written
// schema drifts from the decoder the first time a field is added, and a
// schema that disagrees with the program is worse than none: it tells the
// user their file is fine when Owl will refuse it.
//
// The shapes carry the field names and the types; everything a `yaml` tag
// cannot say - that `apiVersion` is a fixed string, that a duration is
// written like `4h`, that `maxParallelRuns` is read as whatever was written -
// is said here, beside the parser rule it mirrors. Each of those is a place
// the schema could drift from the parser, so each names the function it
// follows.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/credential"
)

// SchemaDialect is the JSON Schema draft the schemas declare. Draft 07 is
// what the YAML language servers behind editor intellisense support best.
const SchemaDialect = "http://json-schema.org/draft-07/schema#"

// Where the schemas are published. The website copies `website/public`
// verbatim, so the path a schema is written to and the URL it is served from
// are the same thing said twice.
const (
	ProjectSchemaURL = "https://codingowl.dev/schema/project.json"
	DaemonSchemaURL  = "https://codingowl.dev/schema/daemon.json"
)

// ProjectSchema is the schema of a Project's configuration file.
func ProjectSchema() ([]byte, error) {
	return render(reflect.TypeOf(projectFile{}), ProjectSchemaURL,
		"Coding Owl project configuration",
		"A Project's `.coding-owl.yaml`, or the `config.yaml` under its "+
			"directory in the configuration home. See "+
			"https://github.com/vojtechmares/coding-owl#project-configuration.")
}

// DaemonSchema is the schema of the daemon's own configuration file.
func DaemonSchema() ([]byte, error) {
	return render(reflect.TypeOf(globalFile{}), DaemonSchemaURL,
		"Coding Owl daemon configuration",
		"The daemon's `config.yaml` in the configuration home. See "+
			"https://github.com/vojtechmares/coding-owl#daemon-configuration.")
}

// render builds one file kind's schema and encodes it. The output is indented
// and ends in a newline, because it is a file in the repository that a person
// reads in a diff.
func render(t reflect.Type, id, title, description string) ([]byte, error) {
	root := schemaOf(t, "")
	root["$schema"] = SchemaDialect
	root["$id"] = id
	root["title"] = title
	root["description"] = description
	// apiVersion is the one setting every file must carry: checkAPIVersion
	// refuses a file without it rather than guessing (ADR-0014).
	root["required"] = []string{"apiVersion"}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the schema of %s: %w", t.Name(), err)
	}
	return append(out, '\n'), nil
}

// schemaOf is the schema of one Go type at one place in the file. path is the
// dotted `yaml` path to it, with `*` for the thing inside a list or a map, and
// is what the rules below are keyed on.
func schemaOf(t reflect.Type, path string) map[string]any {
	if s := instead(path); s != nil {
		return s
	}
	out := generated(t, path)
	for keyword, value := range alsoSay(path) {
		out[keyword] = value
	}
	return out
}

// generated is the schema a type gives on its own, before any rule.
func generated(t reflect.Type, path string) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		return schemaOf(t.Elem(), path)
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": schemaOf(t.Elem(), join(path, "*")),
		}
	case reflect.Map:
		return map[string]any{
			"type": "object",
			// A map's keys are the user's own words - an Account's name, a
			// phase - so what is described is the value.
			"additionalProperties": schemaOf(t.Elem(), join(path, "*")),
		}
	case reflect.Struct:
		properties := map[string]any{}
		for i := range t.NumField() {
			field := t.Field(i)
			name, ok := yamlName(field)
			if !ok {
				continue
			}
			properties[name] = schemaOf(field.Type, join(path, name))
		}
		return map[string]any{
			"type":       "object",
			"properties": properties,
			// The decoder is strict: a key the shape does not have is refused
			// by name rather than ignored (decodeStrict). An editor should say
			// so where it is typed.
			"additionalProperties": false,
		}
	default:
		// `any`, which is a field read as whatever was written. Every one of
		// them has a rule below; this is what is left if one is ever added
		// without one, and it accepts anything rather than refusing what the
		// parser allows.
		return map[string]any{}
	}
}

// yamlName is the key a field is written under, and whether it is written at
// all.
func yamlName(f reflect.StructField) (string, bool) {
	tag, ok := f.Tag.Lookup("yaml")
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return "", false
	}
	return name, true
}

func join(path, step string) string {
	if path == "" {
		return step
	}
	return path + "." + step
}

// duration is how every duration in a configuration file is written: Go's own
// `time.ParseDuration` grammar, which is what every one of them is read with.
// A negative one is left out because each parser refuses a duration that is
// not positive - an Agent needs time to work.
const durationPattern = `^[0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h)` +
	`([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))*$`

// durationPaths are the settings parsed with time.ParseDuration.
var durationPaths = map[string]string{
	"phases.*.timeout":              "The longest this phase's Agent may run at all, like `4h`.",
	"phases.*.stall":                "The longest this phase's Agent may go without saying anything, like `15m`.",
	"checks.*.timeout":              "The longest this check may run, like `10m`.",
	"verification.timeout":          "The longest the reviewing Agent may take.",
	"graceWindow":                   "How long a Run is given to finish after Owl asks it to stop, like `15m`.",
	"idle.after":                    "How long without keyboard or mouse input counts as idle, like `10m`.",
	"idle.interval":                 "How often the machine is read. At least `50ms`.",
	"garbageCollection.interval":    "How often finished worktrees are reclaimed, like `1h`.",
	"garbageCollection.reviewAfter": "How long a Job may wait for a decision before it is reported.",
}

// instead is the schema that replaces what the type gives, for the settings
// whose Go type says less than the parser does.
func instead(path string) map[string]any {
	if describe, ok := durationPaths[path]; ok {
		return map[string]any{
			"type":        "string",
			"pattern":     durationPattern,
			"description": describe,
		}
	}
	switch path {
	case "apiVersion":
		// checkAPIVersion accepts this one string and no other.
		return map[string]any{
			"type":        "string",
			"const":       APIVersion,
			"description": "The only apiVersion Owl recognises.",
		}

	case "maxParallelRuns":
		return parallel("How many Runs may go at once. At least one; " +
			"stopping the daemon is how nothing runs.")
	case "accounts.*.maxParallel":
		return parallel("How many Runs may draw on this Account at once. " +
			"Unset is unlimited, because the Ceiling already governs burn rate.")

	case "phases":
		// parsePhases refuses a phase Owl does not have, so the schema names
		// the two rather than describing a map.
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				PhasePlan:    schemaOf(reflect.TypeOf(phase{}), "phases.*"),
				PhaseExecute: schemaOf(reflect.TypeOf(phase{}), "phases.*"),
			},
			"additionalProperties": false,
			"description":          "What each phase of a Job runs as, and how long it may take.",
		}

	case "checks.*.expect":
		// parseChecks accepts this one word, or nothing at all.
		return map[string]any{
			"type":        "string",
			"enum":        []string{ExpectEmptyOutput},
			"description": "What the check's success looks like. The default is exit zero.",
		}

	case "credentialStore":
		// credential.ParseKind accepts these two.
		return map[string]any{
			"type":        "string",
			"enum":        []string{string(credential.KindFile), string(credential.KindKeychain)},
			"description": "Where an Account's secret is kept. The default is the keychain on macOS.",
		}

	case "accounts.*.limits":
		// parseAccounts refuses a ceiling Owl does not keep, naming the two it
		// does, so the schema names them too.
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				fiveHourMax: percent("The share of the five-hour window Owl will not schedule past."),
				weeklyMax:   percent("The share of the weekly window Owl will not schedule past."),
			},
			"additionalProperties": false,
			"description": "What this Account is held to, as percentages of its " +
				"rate-limit windows, counting what you spent yourself.",
		}
	}
	return nil
}

// parallel is a number of Runs as parseParallel reads one: a whole number of
// at least one, written as a number or as a string.
func parallel(describe string) map[string]any {
	return map[string]any{
		"anyOf": []any{
			map[string]any{"type": "integer", "minimum": 1},
			map[string]any{"type": "string", "pattern": `^\s*[0-9]+\s*$`},
		},
		"description": describe,
	}
}

// percent is a Ceiling as parsePercent reads one: a share of a window between
// 1 and 100, written with or without the sign.
func percent(describe string) map[string]any {
	return map[string]any{
		"anyOf": []any{
			map[string]any{"type": "number", "exclusiveMinimum": 0, "maximum": 100},
			map[string]any{"type": "string", "pattern": `^\s*([0-9]+(\.[0-9]+)?|\.[0-9]+)\s*%?\s*$`},
		},
		"description": describe,
	}
}

// alsoSay is what a setting's own type does not say: what it is for, and the
// few constraints a parser puts on a value it otherwise takes as written. An
// empty value is left valid wherever the parser leaves it valid, because a
// schema stricter than the program is a squiggle under a file that loads.
func alsoSay(path string) map[string]any {
	switch path {
	case "account":
		return map[string]any{"description": "The Account this Project's Jobs run on."}
	case "branchPrefix":
		return map[string]any{
			// git reads an argument beginning with a dash as an option, which
			// is what Parse refuses. Unset is the default prefix.
			"pattern":     `^$|^[^-]`,
			"description": "What a Job's branch is named under. The default is `owl/`.",
		}
	case "unattendedClauses":
		return map[string]any{"description": "Appended to Owl's standing unattended contract for this Project."}
	case "budgetUSD":
		return map[string]any{
			"minimum":     0,
			"description": "What this Project's Jobs may spend. A spend cap cannot be negative.",
		}
	case "setup":
		return map[string]any{"description": "Shell commands preparing each fresh worktree before an Agent starts."}
	case "setup.*":
		return map[string]any{"minLength": 1}
	case "checks":
		return map[string]any{"description": "The Verification checks that judge a Run's work. All run, and every failure is reported."}
	case "checks.*.name":
		return map[string]any{"minLength": 1, "description": "What the check is called, unique within the Project."}
	case "checks.*.run":
		return map[string]any{"minLength": 1, "description": "The shell command to run."}
	case "verification":
		return map[string]any{"description": "Verification beyond the Project's own checks."}
	case "verification.agent":
		return map[string]any{"description": "Additionally have a fresh Agent review the diff."}
	case "skills":
		return map[string]any{"description": "The Skills Owl fetches and pins for this Project's Agents."}
	case "skills.*.git":
		return map[string]any{"minLength": 1, "description": "The repository the Skill comes from, like `owner/repo`."}
	case "skills.*.ref":
		return map[string]any{"pattern": `^$|^[^-]`, "description": "The ref to pin it to."}
	case "skills.*.auto_update":
		return map[string]any{"description": "Whether Owl moves the pin when the ref does."}
	case "phases.*.model":
		return map[string]any{"description": "What this phase runs as, like `anthropic/claude-opus`. `owl models` lists them."}
	case "phases.*.effort":
		return map[string]any{"description": "How hard this phase thinks, like `xhigh`."}
	case "allowedTools":
		return map[string]any{"description": "What an unattended Agent may do without asking."}
	case "allowedTools.*":
		return map[string]any{"minLength": 1}
	case "claudePath":
		return map[string]any{
			// ParseGlobal refuses a relative path: it would mean something
			// different wherever the daemon happened to be started from.
			"pattern":     `^$|^/`,
			"description": "Where the coding tool is, when the daemon's PATH does not say. An absolute path.",
		}
	case "idle":
		return map[string]any{"description": "When the machine counts as idle, and so when Owl may work."}
	case "idle.requirePower":
		return map[string]any{"description": "Whether the machine must be on AC power."}
	case "garbageCollection":
		return map[string]any{"description": "How often Owl reconciles the worktrees on disk against the Jobs in its database."}
	case "accounts":
		return map[string]any{"description": "What each Account is held to. The key is the Account's name."}
	}
	return map[string]any{}
}
