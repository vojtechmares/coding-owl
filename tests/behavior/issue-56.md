# Issue #56: Unattended Agents are never granted any tool permission

An Agent runs with permission prompts disabled, so anything that would have
asked is denied. Reported at commit `701b936`: nothing granted anything in the
first place. No allowlist was passed, and nothing seeded one into the Account's
own configuration directory, so on a fresh install every edit and most
commands were denied and a Job ended in `blocked` or `review` with an empty
diff. ADR-0006 prefers the tool's own allowlist over skipping permissions, and
nothing implemented it.

The fix is that the Claude Code Driver seeds each Account's configuration
directory with a `settings.json` holding a default allowlist before the first
Run there, and never overwrites a file that exists; that a Project may extend
the allowlist with `allowedTools` in its own file; that a Run records the
permissions its Agent had and `owl jobs show` prints them; and that the stub
agent of the behavioural suite refuses to run without the file. Skipping
permissions outright, and per-Job overrides, are out of scope.

"The default allowlist" is the list ADR-0035 records. "The settings file" is
`settings.json` in the Account's configuration directory.

The config and driver scenarios exercise those packages' own APIs. The
end-to-end scenarios drive the built `owl` binary against a daemon, with the
stub agent standing in for Claude Code.

## Scenarios

### S1 - a fresh Account gets the settings file with the default allowlist
Given an Account whose configuration directory holds no settings file
When the Driver builds an Agent for it
Then the settings file exists, readable by the owner only
And its allowlist is exactly the default allowlist

### S2 - a settings file the user edited is never touched
Given an Account whose settings file the user has written themselves
When the Driver builds an Agent for it
Then the settings file's contents are exactly what the user wrote

### S3 - the invocation carries the permissions in force and passes a Project's own
Given an Account whose settings file allows a list of the user's choosing, and a Project that extends it with `allowedTools`
When the Driver builds an Agent for it
Then the invocation's permissions are the file's list followed by the Project's
And the Project's list is passed to the tool as its allowed tools
And the invocation still disables permission prompts

### S4 - a Project file may extend the allowlist
Given a Project file with an `allowedTools` list
When it is parsed
Then the configuration carries the list in order
And a Project file with an empty entry in the list is refused naming the entry

### S5 - a Run's Agent finds the settings file and is started without prompts
Given a running daemon, a fresh Account, and a pending Job on a Project on that Account
When the Job is run
Then the Agent was started with permission prompts disabled
And the settings file in the Agent's configuration directory allows the default allowlist

### S6 - `owl jobs show` prints the permissions the Run had
Given the Run of S5, on a Project whose file extends the allowlist with one rule
When `owl jobs show <job>` is read
Then it lists, for that Run, every rule of the default allowlist and the Project's rule

### S7 - a Run leaves an edited settings file alone
Given a running daemon, an Account whose settings file the user has written themselves, and a pending Job on that Account
When the Job is run
Then the settings file's contents are exactly what the user wrote
And the permissions the Run had are the user's list

### S8 - the decision record names the default allowlist and who owns the file
Given the decision record for Agent permissions
When it is read
Then it names every rule of the default allowlist
And it says the settings file is the user's once it exists
