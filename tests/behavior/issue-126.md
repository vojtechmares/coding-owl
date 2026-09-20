# Issue #126: owl project setup, and owl project add pointing at it

A Project with no `account` cannot run a single Job (ADR-0023), and nothing
told the user so. `owl project add` registered the repository and stopped;
`owl project show` printed `account: (none)`; and the only way forward was to
know that a configuration file exists, where ADR-0014 looks for one, and what
to write in it.

The change is a new `owl project setup [<name>|<path>]` that walks the user
through the one setting a Project cannot run without. It lists the registered
Accounts and asks which one, asks whether the file should be the in-repo
`.coding-owl.yaml` that gets committed or the config-home fallback that does
not, and writes `apiVersion` and `account` and nothing else. It then points at
the configuration documentation for everything else a file can carry, in words
a coding agent could be handed. And `owl project add` now ends by saying to run
it, when the Project it just registered has no configuration anywhere.

The two locations differ in a way the command has to say out loud: the
config-home form is read from disk, so it is in force the moment it is written,
while an in-repo form is read from the Project's base branch with
`git show <base>:<path>` and never from the working tree (ADR-0014). Writing
`.coding-owl.yaml` therefore changes nothing until it is committed. The command
writes it and says so rather than committing on the user's behalf, because a
commit in someone's repository is not a thing a setup command should make
without being asked.

The answers are read from standard input, so the command works when it is
piped as well as when it is typed, and running out of input ends it with a
message rather than a hang. That is what makes these scenarios drivable: they
run the built binary with its input written ahead of time.

## Scenarios

### S1 - setup writes the in-repo file, and says it is not in force yet
Given a running daemon, an Account `work`, and a registered Project `api` with no configuration file anywhere
When `owl project setup api` runs, answered with `work` and then the in-repo location
Then it exits 0
And `<project>/.coding-owl.yaml` carries `apiVersion: codingowl.dev/v1` and `account: work`, and no other setting
And the output says the file has to be committed to the base branch before it takes effect
And `owl project show api` still reports no configuration, because the working tree is not where it is read from

### S2 - setup writes the config-home fallback, which is in force at once
Given the same daemon, Account and Project
When `owl project setup api` runs, answered with `work` and then the config-home location
Then `<config home>/coding-owl/api/config.yaml` carries the same two settings
And nothing is written inside the repository
And `owl project show api` reports the Account `work` straight away

### S3 - the Project can be named by a path inside it
Given a registered Project `api` with no configuration
When `owl project setup <project>/sub/deeper` runs and is answered
Then the Project `api` is the one configured

### S4 - a Project that already has a configuration file is refused
Given a registered Project whose base branch carries a `.coding-owl.yaml`
When `owl project setup api` runs
Then it exits non-zero
And stderr names the file already in force and says setup will not overwrite it
And nothing is written

### S5 - setup with no Account registered is refused and says what to do
Given a running daemon with no Account registered, and a registered Project
When `owl project setup api` runs
Then it exits non-zero
And stderr says there is no Account to run on and names `owl account add`
And nothing is written

### S6 - setup with no input fails rather than hanging
Given a running daemon, an Account, and a registered Project
When `owl project setup api` runs with its standard input closed
Then it exits non-zero rather than waiting
And stderr says it needed an answer and got none
And nothing is written

### S7 - an answer that is not one of the choices is asked again
Given a running daemon, the Accounts `work` and `personal`, and a registered Project
When `owl project setup api` is answered `9`, then the choice for `work`, then the in-repo location
Then it says `9` is not one of the choices
And it goes on to write the file for `work`

### S8 - a Project that is not registered is refused
Given a running daemon and a registered Project `api`
When `owl project setup ghost` runs, and then `owl project setup` for a directory inside no Project
Then each exits non-zero
And each says on stderr what it could not find, naming what it was given

### S9 - project add says to run setup when the new Project has no configuration
Given a running daemon and a repository whose base branch carries no configuration file
When `owl project add <path>` runs
Then it exits 0 and still says what it registered and where
And it says no configuration file was found, and names `owl project setup <name>` with the name it just registered

### S10 - project add says nothing about setup when the Project is already configured
Given a repository whose base branch carries a `.coding-owl.yaml` naming an Account
When `owl project add <path>` runs
Then it exits 0
And its output does not mention `owl project setup`

### S11 - setup points at the documentation for everything else
Given a successful run of S1 or S2
When its output is read to the end
Then it names where the rest of a Project's configuration is documented
And it says the rest can be filled in afterwards, in words that could be handed to a coding agent
