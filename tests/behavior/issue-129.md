# Issue #129: JSON schemas for the project and daemon configuration files

Every Owl configuration file is decoded strictly: a key the Go shape does not
have is refused by name rather than ignored (ADR-0014). An editor knows none of
that, so a misspelled `branchPrefix` is a refusal at `owl project show` rather
than a squiggle while it is being typed.

The change is that the two on-disk shapes - `projectFile` and `globalFile` in
`internal/config/config.go` - are the source of a pair of generated JSON
schemas, published with the website at `https://codingowl.dev/schema/project.json`
and `https://codingowl.dev/schema/daemon.json`. The generator is wired into
`make generate` beside `buf generate`, and CI fails when the committed schemas
are not what the structs would produce, the way it already does for the
generated protobuf code. The docs show the `# yaml-language-server: $schema=`
line that points an editor at them.

The schemas are generated rather than written by hand, so that they cannot
drift from the decoder the first time a field is added; and they say
`additionalProperties: false` everywhere, because that is what the decoder's
`KnownFields(true)` means. They are Draft 07, which is what the YAML language
servers behind editor intellisense support best, and what SchemaStore's own
schemas use.

The scenarios read the repository as an editor, a developer and CI would: the
published files on disk, the generator's own output, and the text of the
Makefile, the CI workflow and the docs. No scenario reaches the network - what
is on disk is what the site deploy publishes.

## Scenarios

### S1 - the two schemas are published with the site
Given the repository
When `website/public/schema/project.json` and `website/public/schema/daemon.json` are read
Then each parses as JSON
And each declares Draft 07 in `$schema`
And their `$id`s are `https://codingowl.dev/schema/project.json` and `https://codingowl.dev/schema/daemon.json`
And each carries a `title` and a `description` saying which file it is for

### S2 - the committed schemas are what the structs produce
Given the repository
When the generator runs
Then neither file on disk changes

### S3 - make generate regenerates them
Given the repository
When `Makefile`'s `generate` target is read
Then it runs the schema generator as well as `buf generate`
And running that generator into an empty directory writes exactly the two files, byte-identical to the committed ones

### S4 - apiVersion is required and pinned
Given either schema
When its `apiVersion` property is read
Then it is a `const` of `codingowl.dev/v1`
And `apiVersion` is listed in `required`

### S5 - a key the decoder would refuse is invalid in the schema
Given either schema
When every object in it is read
Then each says `additionalProperties: false`, which is what the decoder's strict mode means
And neither schema names a key that is not a `yaml` tag on the shape it was generated from

### S6 - each schema describes its own file kind and not the other
Given the two schemas
When their top-level properties are read
Then the project schema has `account`, `branchPrefix`, `checks`, `phases`, `setup`, `skills`, `verification`, `allowedTools`, `unattendedClauses`, `budgetUSD` and `maxParallelRuns`, and no others
And the daemon schema has `claudePath`, `credentialStore`, `accounts`, `idle`, `graceWindow`, `garbageCollection`, `phases` and `maxParallelRuns`, and no others
And neither has a top-level key belonging to the other: the project schema has no `claudePath` or `accounts`, and the daemon schema has no `checks` or `account`

### S7 - the settings read as whatever was written take a number or a string
Given the two schemas
When `maxParallelRuns` and `accounts.*.maxParallel` are read
Then each accepts an integer of at least one, or a string of digits
And neither is left as an unconstrained `any`, which would offer no help at all

### S8 - an Account's limits name only the ceilings Owl keeps
Given the daemon schema
When `accounts.*.limits` is read
Then its properties are exactly `fiveHourMax` and `weeklyMax`
And it says `additionalProperties: false`, so a ceiling Owl does not keep is refused in the editor as it is on load

### S9 - the closed sets Owl keeps are enums
Given the two schemas
When the settings whose values Owl fixes are read
Then a check's `expect` is an enum whose only value is `empty_output`
And `phases` has exactly the properties `plan` and `execute`, and no others
And the daemon schema's `credentialStore` is an enum of `keychain` and `file`
And so an editor refuses in place what the parser would refuse on load

### S10 - a duration is described as one
Given the two schemas
When `graceWindow`, `idle.after`, `garbageCollection.interval` and a phase's `timeout` and `stall` are read
Then each is a string with a pattern a Go duration matches, and `4h` matches it while `soon` does not

### S11 - the configuration the README documents satisfies its schema
Given the project and daemon YAML examples in README.md's Configuration section
When each key in each example is looked up in the matching schema, at its own nesting
Then every one of them is a property that schema declares
And the same examples are accepted by Owl's own parser, so the schema and the decoder agree about one real document

### S12 - CI refuses a stale schema
Given `.github/workflows/ci.yml`
When its steps are read
Then one regenerates the schemas and fails on a difference, as the step for the generated protobuf code already does

### S13 - the docs say how to point an editor at them
Given the documentation
When the configuration section is read
Then it shows a `# yaml-language-server: $schema=` line for a Project's file and for the daemon's
And each names the published URL from S1
