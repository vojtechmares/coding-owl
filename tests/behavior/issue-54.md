# Issue #54: Non-strict YAML lets typos and cross-file keys pass silently

Both configuration files - a Project's `.coding-owl.yaml` and the daemon's own
`config.yaml` - were decoded into one shared shape with no check for keys that
shape does not have. Reported at commit `701b936`: a misspelled
`maxParalellRuns` silently meant the default, a `checks:` block in the daemon's
file was silently dropped, and `credentialStore:` in a Project's file was
accepted and ignored.

The fix is that each file kind has an on-disk shape of its own, decoded with
strict field checking, so that a key the shape does not have - a typo, or a
key that belongs to the other file - is refused with an error that names the
key and the file it came from. No key name or default changes (out of scope).

The config scenarios exercise that package's own API. The end-to-end scenarios
drive the built `owl` binary and the daemon.

## Scenarios

### S1 - a Project file with an unknown top-level key is refused by name
Given a Project file with `maxParalellRuns: 2` beside a valid `apiVersion`
When it is parsed
Then it is refused
And the error names `maxParalellRuns` and the file

### S2 - a global file with a Project-only block is refused by name
Given a daemon file with a `checks:` block beside a valid `apiVersion`
When it is parsed
Then it is refused
And the error names `checks` and the file

### S3 - a Project file with a global-only key is refused by name
Given a Project file with `credentialStore: file` beside a valid `apiVersion`
When it is parsed
Then it is refused
And the error names `credentialStore` and the file

### S4 - an unknown key inside a block is refused by name
Given a daemon file whose `idle` block has `afterr: 5m`, and a Project file whose first check has `runn: make test`
When each is parsed
Then each is refused
And each error names the misspelled key

### S5 - `owl project show` reports an unknown key in the Project's file
Given a running daemon and a registered Project whose committed `.coding-owl.yaml` has an unknown top-level key
When `owl project show <name>` is run
Then it exits non-zero
And its error names the key and `.coding-owl.yaml`

### S6 - the daemon refuses to start on a file with a key it does not have
Given a daemon file with a `checks:` block
When `owl daemon run` is started
Then it exits non-zero
And what it printed names `checks` and the file
