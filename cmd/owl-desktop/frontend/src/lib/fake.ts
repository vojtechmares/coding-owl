// A daemon that is not there: enough made-up state to see every part of the
// app at once, for looking at the design without queueing real work (#36).
//
// It stands in one layer below the app rather than beside it. Wails' generated
// bindings are calls onto `window.go`, and its runtime is calls onto
// `window.runtime`; this file puts both of those there. So `lib/api.ts`, the
// bindings and every view run exactly as they do against a real daemon, and
// nothing outside this file knows the difference.
//
// It is reached with `make desktop-fake`, never by a release build: `install`
// is called behind `import.meta.env.VITE_FAKE`, which is a constant at build
// time, so an ordinary build drops this module entirely.

type Handler = (data: unknown) => void;

const listeners = new Map<string, Set<Handler>>();

function emit(name: string, data: unknown) {
  for (const h of listeners.get(name) ?? []) h(data);
}

// The clock the fake runs on. Times are written as an offset from when the app
// opened, so a Run started "12 minutes ago" is still 12 minutes ago tomorrow.
const opened = Date.now();
const at = (minutesAgo: number) => new Date(opened - minutesAgo * 60_000).toISOString();
// Go's zero time, which is what the daemon sends for a Run that has not ended.
const never = "0001-01-01T00:00:00Z";

const projects = [
  { Name: "coding-owl", Path: "/Users/vojta/work/me/coding-owl", BaseBranch: "main", Registered: at(60 * 24 * 12) },
  { Name: "acceptmarkdown", Path: "/Users/vojta/work/acceptmarkdown", BaseBranch: "main", Registered: at(60 * 24 * 6) },
  { Name: "mares.cz", Path: "/Users/vojta/work/me/mares.cz", BaseBranch: "trunk", Registered: at(60 * 24 * 2) },
];

interface FakeJob {
  ID: number;
  Source: string;
  SourceRef: string;
  Project: string;
  Prompt: string;
  State: string;
  Branch: string;
  Worktree: string;
  Planned: boolean;
  Plan: string;
  Reason: string;
  TTL: number;
  Account: string;
  Position: number;
  Created: string;
}

function job(j: Partial<FakeJob> & { ID: number; Project: string; Prompt: string; State: string }): FakeJob {
  return {
    Source: "cli",
    SourceRef: `add/${j.ID}`,
    Branch: j.State === "pending" ? "" : `owl/job-${j.ID}`,
    Worktree: j.State === "pending" ? "" : `/Users/vojta/.local/share/coding-owl/worktrees/job-${j.ID}`,
    Planned: true,
    Plan: "",
    Reason: "",
    TTL: 3,
    Account: "personal",
    Position: 0,
    Created: at(90),
    ...j,
  };
}

const plan = `# Plan

Rebase the Job branch onto the base branch at the start of every Run, so a Run
never reviews work against a base that has moved (ADR-0016).

1. Read the base branch from the Project's configuration.
2. Fetch it, and rebase the Job branch onto it before the Agent starts.
3. On a conflict, stop the Run and block the Job with the conflicting paths as
   the reason. A conflict is a decision, and the Agent is not the one to make
   it.
4. Cover the three cases in tests: nothing to do, a clean rebase, a conflict.`;

const handoff = `# Handoff

## What this Job is for

Every Run starts from the Job branch, and the base branch moves underneath it
between Runs. Rebasing at the start of a Run is what keeps Verification honest.

## What is done

- The rebase itself, in \`internal/git.Rebase\`.
- The clean and nothing-to-do paths, with tests.

## What is left

- The conflict path stops the Run but does not yet name the conflicting files
  in the Job's reason, so the queue says "blocked" without saying on what.

## What I would not do again

Do not try to resolve a conflict from inside the Run. It was tried in attempt 1
and produced a branch nobody could review.`;

const jobs: FakeJob[] = [
  job({
    ID: 41,
    Project: "coding-owl",
    Prompt: "Rebase the Job branch onto the base branch at the start of every Run",
    State: "active",
    Plan: plan,
    Created: at(72),
  }),
  job({
    ID: 39,
    Project: "coding-owl",
    Prompt: "Report a malformed prompt heading instead of dropping it",
    State: "review",
    TTL: 2,
    Created: at(210),
  }),
  job({
    ID: 38,
    Project: "acceptmarkdown",
    Prompt: "Accept a table with a missing trailing pipe, the way every other parser does",
    State: "review",
    TTL: 3,
    Created: at(260),
  }),
  job({
    ID: 37,
    Project: "coding-owl",
    Prompt: "Clear stale git locks before a Run starts",
    State: "blocked",
    Reason: "rebase onto main left conflicts in internal/git/rebase.go",
    TTL: 1,
    Created: at(320),
  }),
  job({
    ID: 33,
    Project: "mares.cz",
    Prompt: "Move the writing index to a content collection",
    State: "exhausted",
    Reason: "three attempts, and Verification failed each time on `pnpm check`",
    TTL: 0,
    Account: "work",
    Created: at(60 * 20),
  }),
  job({
    ID: 44,
    Project: "coding-owl",
    Prompt: "Name a model by its vendor, and let each Driver declare what it serves",
    State: "pending",
    Position: 1,
    Created: at(38),
  }),
  job({
    ID: 45,
    Project: "coding-owl",
    Prompt: "Bound every Run by a timeout and a stall limit",
    State: "pending",
    Position: 2,
    Created: at(30),
  }),
  job({
    ID: 46,
    Project: "acceptmarkdown",
    Prompt: "Add a fuzz target for the table parser",
    State: "pending",
    Position: 3,
    Account: "work",
    Created: at(21),
  }),
  job({
    ID: 47,
    Project: "mares.cz",
    Prompt: "Cache the OG images at the edge instead of rebuilding them each deploy",
    State: "pending",
    Position: 4,
    Created: at(9),
  }),
  job({
    ID: 30,
    Project: "coding-owl",
    Prompt: "Default a Job to three attempts rather than ten",
    State: "done",
    TTL: 2,
    Created: at(60 * 26),
  }),
  job({
    ID: 28,
    Project: "acceptmarkdown",
    Prompt: "Keep the space before a reference link",
    State: "done",
    TTL: 3,
    Created: at(60 * 31),
  }),
  job({
    ID: 26,
    Project: "mares.cz",
    Prompt: "Try the dark theme again",
    State: "cancelled",
    Reason: "dropped by hand",
    TTL: 3,
    Created: at(60 * 40),
  }),
];

const byId = (id: number) => jobs.find((j) => j.ID === id);

function run(r: Partial<Record<string, unknown>> & { ID: number; JobID: number }) {
  return {
    Attempt: 1,
    Started: at(72),
    Ended: never,
    Outcome: "",
    Error: "",
    ExitCode: -1,
    LogPath: `/Users/vojta/.local/state/coding-owl/runs/${r.ID}.jsonl`,
    Phase: "execute",
    Skills: [],
    Permissions: ["Bash(go test:*)", "Bash(git:*)", "Edit", "Write"],
    SystemPrompt: "",
    Paused: false,
    Stage: "agent",
    ...r,
  };
}

const runs: Record<number, ReturnType<typeof run>[]> = {
  41: [
    run({ ID: 118, JobID: 41, Attempt: 1, Phase: "plan", Started: at(74), Ended: at(72), Outcome: "succeeded", ExitCode: 0 }),
    run({ ID: 119, JobID: 41, Attempt: 2, Phase: "execute", Started: at(12), Stage: "agent" }),
  ],
  39: [
    run({ ID: 115, JobID: 39, Attempt: 1, Phase: "plan", Started: at(214), Ended: at(210), Outcome: "succeeded", ExitCode: 0 }),
    run({ ID: 116, JobID: 39, Attempt: 2, Phase: "execute", Started: at(210), Ended: at(184), Outcome: "succeeded", ExitCode: 0 }),
  ],
  38: [run({ ID: 114, JobID: 38, Attempt: 1, Started: at(260), Ended: at(241), Outcome: "succeeded", ExitCode: 0 })],
  37: [
    run({
      ID: 110,
      JobID: 37,
      Attempt: 1,
      Started: at(320),
      Ended: at(318),
      Outcome: "failed",
      ExitCode: 1,
      Stage: "rebase",
      Error: "rebase onto main left conflicts in internal/git/rebase.go",
    }),
  ],
  33: [
    run({ ID: 96, JobID: 33, Attempt: 1, Started: at(60 * 20), Ended: at(60 * 19), Outcome: "failed", ExitCode: 1, Error: "verification failed" }),
    run({ ID: 99, JobID: 33, Attempt: 2, Started: at(60 * 18), Ended: at(60 * 17), Outcome: "failed", ExitCode: 1, Error: "verification failed" }),
    run({ ID: 104, JobID: 33, Attempt: 3, Started: at(60 * 16), Ended: at(60 * 15), Outcome: "failed", ExitCode: 1, Error: "verification failed" }),
  ],
  30: [run({ ID: 92, JobID: 30, Attempt: 1, Started: at(60 * 26), Ended: at(60 * 25), Outcome: "succeeded", ExitCode: 0 })],
  28: [run({ ID: 88, JobID: 28, Attempt: 1, Started: at(60 * 31), Ended: at(60 * 30), Outcome: "succeeded", ExitCode: 0 })],
  26: [],
  44: [],
  45: [],
  46: [],
  47: [],
};

const checks: Record<number, unknown[]> = {
  39: [
    { Name: "build", Command: "go build ./...", Passed: true, ExitCode: 0, Output: "", Reason: "", Verifier: "command" },
    {
      Name: "test",
      Command: "go test ./...",
      Passed: true,
      ExitCode: 0,
      Output: "ok  \tgithub.com/vojtechmares/coding-owl/internal/prompt\t0.412s\nok  \tgithub.com/vojtechmares/coding-owl/internal/queue\t1.882s",
      Reason: "",
      Verifier: "command",
    },
    {
      Name: "does what was asked",
      Command: "",
      Passed: true,
      ExitCode: 0,
      Output: "A malformed heading is reported with its line number and the file is left alone, which is what the prompt asked for.",
      Reason: "",
      Verifier: "agent",
    },
  ],
  33: [
    { Name: "build", Command: "pnpm build", Passed: true, ExitCode: 0, Output: "", Reason: "", Verifier: "command" },
    {
      Name: "check",
      Command: "pnpm check",
      Passed: false,
      ExitCode: 2,
      Reason: "exit 2",
      Verifier: "command",
      Output:
        "src/pages/writing/index.astro:14:22 - error ts(2339): Property 'slug' does not exist on type 'CollectionEntry<\"writing\">'.\n\nFound 1 error in 1 file.",
    },
  ],
};

const diffs: Record<number, unknown> = {
  39: {
    Files: [
      { Path: "internal/prompt/parse.go", Insertions: 34, Deletions: 6 },
      { Path: "internal/prompt/parse_test.go", Insertions: 88, Deletions: 0 },
      { Path: "CHANGELOG.md", Insertions: 3, Deletions: 0 },
    ],
    Insertions: 125,
    Deletions: 6,
  },
  41: {
    Files: [
      { Path: "internal/git/rebase.go", Insertions: 71, Deletions: 12 },
      { Path: "internal/git/rebase_test.go", Insertions: 140, Deletions: 0 },
      { Path: "internal/run/run.go", Insertions: 18, Deletions: 4 },
      { Path: "docs/adr/0016-rebase-job-branch-each-run.md", Insertions: 9, Deletions: 2 },
    ],
    Insertions: 238,
    Deletions: 18,
  },
};

const systemPrompt = `You are working in a git worktree of the project "coding-owl", on the branch
owl/job-41, which is rebased onto main.

Do the work described below. When you are done, leave the worktree in a state
that builds and passes the project's checks, and write a handoff at
.coding-owl/HANDOFF.md saying what you did and what is left.

Nobody is watching. If you reach a decision that is not yours to make, stop and
say so in the handoff rather than guessing.`;

const overview = () => ({
  Running: [{ Run: runs[41][1], Job: byId(41) }],
  Counts: [
    { State: "pending", Count: 4 },
    { State: "active", Count: 1 },
    { State: "review", Count: 2 },
    { State: "blocked", Count: 1 },
    { State: "exhausted", Count: 1 },
    { State: "done", Count: 2 },
  ],
  Awaiting: jobs.filter((j) => j.State === "review"),
  Blocked: jobs.filter((j) => j.State === "blocked"),
  Exhausted: jobs.filter((j) => j.State === "exhausted"),
  Unfinished: [
    {
      Job: 33,
      Project: "mares.cz",
      Path: "/Users/vojta/.local/share/coding-owl/worktrees/job-33",
      Reason: "the Job is exhausted but the worktree has uncommitted changes",
      Since: 60 * 60 * 15 * 1e9,
    },
  ],
  Machine: {
    Read: true,
    Idle: true,
    Since: 41 * 60 * 1e9,
    OnPower: true,
    Detail: "",
  },
  Holding: "",
  Accounts: [
    {
      Name: "personal",
      Windows: [
        { Name: "5h", Read: true, Utilization: 38.4, Ceiling: 80, Resets: at(-127) },
        { Name: "week", Read: true, Utilization: 61, Ceiling: 80, Resets: at(-60 * 51) },
      ],
      Waiting: "",
      Until: never,
    },
    {
      Name: "work",
      Windows: [
        { Name: "5h", Read: true, Utilization: 84, Ceiling: 80, Resets: at(-64) },
        { Name: "week", Read: true, Utilization: 44.5, Ceiling: 80, Resets: at(-60 * 39) },
      ],
      Waiting: "work is over its 5h ceiling; Owl will pick its Jobs up again in 1h 4m",
      Until: at(-64),
    },
  ],
  PassedOver: [
    { Job: byId(46), Reason: "its Account, work, is over the 5h ceiling" },
  ],
});

const skills: Record<string, unknown[]> = {
  "coding-owl": [
    {
      Name: "tdd",
      Source: "github.com/vojtechmares/owl-skills",
      Ref: "v1.4.0",
      Commit: "9f1c0ab4d21e8a37",
      Digest: "sha256:5b9e…",
      AutoUpdate: false,
    },
    {
      Name: "conventional-commits",
      Source: "github.com/vojtechmares/owl-skills",
      Ref: "main",
      Commit: "2ad77e0c9b114f6a",
      Digest: "sha256:1c22…",
      AutoUpdate: true,
    },
    {
      Name: "go-review",
      Source: "github.com/anthropics/skills",
      Ref: "",
      Commit: "",
      Digest: "",
      AutoUpdate: false,
    },
  ],
  acceptmarkdown: [
    {
      Name: "tdd",
      Source: "github.com/vojtechmares/owl-skills",
      Ref: "v1.4.0",
      Commit: "9f1c0ab4d21e8a37",
      Digest: "sha256:5b9e…",
      AutoUpdate: false,
    },
  ],
  "mares.cz": [],
};

const driverModels = {
  Driver: "claude-code",
  Models: [
    { Name: "opus", Alias: true, About: "The most capable model, for planning and work that has to be right." },
    { Name: "sonnet", Alias: true, About: "The everyday model: most Runs use this." },
    { Name: "haiku", Alias: true, About: "The fastest and cheapest, for Verification and small edits." },
    { Name: "claude-opus-5", Alias: false, About: "Pinned: stays on this model whatever the vendor ships next." },
    { Name: "claude-sonnet-5", Alias: false, About: "Pinned." },
  ],
};

const conversations = [
  { ID: 3, Title: "Why was job 37 blocked?", Model: "claude-sonnet-5", Created: at(46), Updated: at(44) },
  { ID: 2, Title: "What did verification say about 33?", Model: "claude-sonnet-5", Created: at(60 * 15), Updated: at(60 * 15) },
  { ID: 1, Title: "Summarise last night", Model: "claude-opus-5", Created: at(60 * 9), Updated: at(60 * 9) },
];

const messages: Record<number, { Role: string; Text: string; Created: string }[]> = {
  3: [
    { Role: "user", Text: "Why was job 37 blocked?", Created: at(46) },
    {
      Role: "assistant",
      Text: "Run 110 stopped before the Agent started. Owl rebases a Job branch onto the base branch at the start of every Run, and that rebase left conflicts in internal/git/rebase.go.\n\nA conflict is a decision, so the Run stopped rather than guessing: the Job is blocked with that as its reason and its branch is still there. Resolve it in the worktree and the next Run will carry on, or drop the Job if the work has been overtaken.",
      Created: at(44),
    },
  ],
  2: [
    { Role: "user", Text: "What did verification say about 33?", Created: at(60 * 15) },
    {
      Role: "assistant",
      Text: "The same thing three times: `pnpm check` exited 2 on src/pages/writing/index.astro:14, where the code reads .slug off a CollectionEntry that no longer has one. The build passed each time, so only the type check stood in the way.\n\nThe Job is out of attempts. It is a two-line fix, and the branch owl/job-33 already has the rest of the work on it.",
      Created: at(60 * 15),
    },
  ],
  1: [
    { Role: "user", Text: "Summarise last night", Created: at(60 * 9) },
    {
      Role: "assistant",
      Text: "Four Runs, on three Jobs.\n\n- 30 and 28 finished and are done.\n- 39 is waiting for you to accept or drop it: verification passed, 125 lines added across three files.\n- 33 spent its last attempt and is exhausted.\n\nOwl was idle from 01:12 and stopped at 06:58 when you touched the machine.",
      Created: at(60 * 9),
    },
  ],
};

const chatModels = [
  { Provider: "anthropic", ID: "claude-opus-5" },
  { Provider: "anthropic", ID: "claude-sonnet-5" },
  { Provider: "anthropic", ID: "claude-haiku-4-5-20251001" },
  { Provider: "openai", ID: "gpt-5.2" },
];

// The log lines a followed Run streams, which arrive one at a time so that the
// log looks like what it is: something still happening.
const logLines = [
  '{"t":"14:02:11","event":"run.start","run":119,"job":41,"attempt":2,"phase":"execute"}',
  '{"t":"14:02:11","event":"git.fetch","remote":"origin","base":"main"}',
  '{"t":"14:02:13","event":"git.rebase","onto":"main","result":"clean","commits":3}',
  '{"t":"14:02:13","event":"skills.install","count":3,"names":["tdd","conventional-commits","go-review"]}',
  '{"t":"14:02:14","event":"agent.start","driver":"claude-code","model":"claude-sonnet-5","effort":"medium"}',
  '{"t":"14:02:41","event":"agent.tool","name":"Read","path":"internal/git/rebase.go"}',
  '{"t":"14:03:02","event":"agent.tool","name":"Read","path":"internal/run/run.go"}',
  '{"t":"14:03:38","event":"agent.text","chars":412}',
  '{"t":"14:04:07","event":"agent.tool","name":"Edit","path":"internal/git/rebase.go","added":71,"removed":12}',
  '{"t":"14:05:19","event":"agent.tool","name":"Write","path":"internal/git/rebase_test.go","added":140}',
  '{"t":"14:06:02","event":"agent.tool","name":"Bash","cmd":"go test ./internal/git/..."}',
  '{"t":"14:06:29","event":"agent.tool.result","name":"Bash","exit":1,"lines":18}',
  '{"t":"14:07:11","event":"agent.text","chars":260}',
  '{"t":"14:07:44","event":"agent.tool","name":"Edit","path":"internal/git/rebase.go","added":6,"removed":4}',
  '{"t":"14:08:20","event":"agent.tool","name":"Bash","cmd":"go test ./internal/git/..."}',
  '{"t":"14:08:51","event":"agent.tool.result","name":"Bash","exit":0,"lines":4}',
  '{"t":"14:09:30","event":"agent.tool","name":"Edit","path":"internal/run/run.go","added":18,"removed":4}',
  '{"t":"14:10:12","event":"agent.tool","name":"Bash","cmd":"go build ./..."}',
  '{"t":"14:10:38","event":"agent.tool.result","name":"Bash","exit":0,"lines":0}',
];

// Following a Run plays the log out rather than dumping it: a design review
// should see the view that is moving, because that is the one that is hard to
// get right.
const following = new Map<number, ReturnType<typeof setInterval>>();

function followLog(runId: number) {
  stopLog(runId);
  let at = 0;
  const timer = setInterval(() => {
    if (at >= logLines.length) {
      // Run 119 is still going, so its log does not end; an older one does.
      if (runId !== 119) {
        emit("run:log:end", { runId, error: "" });
        stopLog(runId);
      }
      return;
    }
    emit("run:log", { runId, line: logLines[at] });
    at += 1;
  }, 220);
  following.set(runId, timer);
  return Promise.resolve();
}

function stopLog(runId: number) {
  const timer = following.get(runId);
  if (timer !== undefined) {
    clearInterval(timer);
    following.delete(runId);
  }
  return Promise.resolve();
}

// An answer arrives the way a real one does, a few characters at a time, so
// that the view is judged while it is filling rather than only when it is
// full.
function say(conversationId: number, text: string) {
  const words = text.split(/(?<=\s)/);
  let at = 0;
  const timer = setInterval(() => {
    if (at >= words.length) {
      clearInterval(timer);
      emit("chat:end", { conversationId, error: "" });
      return;
    }
    emit("chat:delta", { conversationId, text: words[at] });
    at += 1;
  }, 24);
}

let nextConversation = 4;
let asked = false;

const app: Record<string, (...args: never[]) => Promise<unknown>> = {
  Status: async () => ({
    running: true,
    version: "0.1.0",
    uptime: "6h 41m",
    socketPath: "/Users/vojta/Library/Application Support/coding-owl/owl.sock",
    error: "",
  }),
  Projects: async () => projects,
  Overview: async () => overview(),
  Jobs: async (...args: never[]) => {
    const all = args[0] as unknown as boolean;
    return all ? jobs : jobs.filter((j) => j.State === "pending");
  },
  Job: async (...args: never[]) => {
    const id = args[0] as unknown as number;
    const j = byId(id) ?? jobs[0];
    return {
      Job: j,
      Runs: runs[j.ID] ?? [],
      SystemPrompt: systemPrompt,
      NextSystemPrompt: "",
      VerifierSystemPrompt:
        "You are checking work you did not do. Run the project's checks, then say whether the change does what was asked. Do not fix anything.",
      Phases: [
        { Phase: "plan", Model: "opus", ModelFrom: "project", Effort: "high", EffortFrom: "default", Timeout: 1800e9, TimeoutFrom: "default", Stall: 300e9, StallFrom: "default" },
        { Phase: "execute", Model: "sonnet", ModelFrom: "default", Effort: "medium", EffortFrom: "default", Timeout: 5400e9, TimeoutFrom: "project", Stall: 600e9, StallFrom: "default" },
        { Phase: "verify", Model: "haiku", ModelFrom: "default", Effort: "low", EffortFrom: "default", Timeout: 900e9, TimeoutFrom: "default", Stall: 300e9, StallFrom: "default" },
      ],
      Checks: checks[j.ID] ?? [],
      Handoff: runs[j.ID]?.length ? handoff : "",
      Diff: diffs[j.ID] ?? { Files: [], Insertions: 0, Deletions: 0 },
    };
  },
  Start: async () => ({ started: true, job: byId(44), run: run({ ID: 120, JobID: 44, Started: at(0) }) }),
  Pause: async () => [{ ...runs[41][1], Paused: true }],
  Resume: async () => [runs[41][1]],
  Accept: async (...args: never[]) => byId(args[0] as unknown as number),
  Drop: async (...args: never[]) => byId(args[0] as unknown as number),
  Skills: async (...args: never[]) => skills[args[0] as unknown as string] ?? [],
  UpdateSkills: async (...args: never[]) => {
    const project = args[0] as unknown as string;
    const all = (skills[project] ?? []) as { AutoUpdate: boolean }[];
    return {
      updated: all.filter((s) => s.AutoUpdate),
      all,
      files: { Manifest: ".coding-owl/skills.yaml", Lock: ".coding-owl/skills.lock", InRepo: true },
    };
  },
  Models: async () => chatModels,
  DriverModels: async () => driverModels,
  Conversations: async () => conversations,
  Conversation: async (...args: never[]) => {
    const id = args[0] as unknown as number;
    // A command waiting to be allowed to run is the one thing in the app that
    // asks a question, so opening the conversation about a blocked Job shows
    // it without the review having to provoke one.
    if (id === 3 && !asked) {
      asked = true;
      setTimeout(() => {
        emit("chat:command", {
          conversationId: 3,
          requestId: "fake-1",
          argv: ["git", "log", "--oneline", "-20", "owl/job-37"],
          directory: "/Users/vojta/work/me/coding-owl",
        });
      }, 700);
    }
    return {
      Conversation: conversations.find((c) => c.ID === id) ?? conversations[0],
      Messages: messages[id] ?? [],
    };
  },
  Send: async () => 0,
  SendTo: async (...args: never[]) => {
    let id = args[0] as unknown as number;
    const asked = args[3] as unknown as string;
    if (!id) {
      id = nextConversation;
      nextConversation += 1;
      conversations.unshift({ ID: id, Title: asked.slice(0, 48), Model: "claude-sonnet-5", Created: at(0), Updated: at(0) });
      messages[id] = [];
    }
    messages[id].push({ Role: "user", Text: asked, Created: at(0) });
    const reply =
      "There is no daemon behind this window: it is the fake the design review runs on, so this answer is the same whatever you ask.\n\nWhat is worth looking at is the shape of it - how a turn reads while it is still arriving, and whether the conversations beside it stay out of the way.";
    setTimeout(() => {
      messages[id].push({ Role: "assistant", Text: reply, Created: at(0) });
      say(id, reply);
    }, 260);
    return id;
  },
  AnswerCommand: async () => undefined,
  FollowLog: async (...args: never[]) => followLog(args[0] as unknown as number),
  StopLog: async (...args: never[]) => stopLog(args[0] as unknown as number),
};

// install puts the fake where the generated bindings look for the real thing.
export function install() {
  const w = window as unknown as Record<string, unknown>;
  w.go = { desktop: { App: app } };
  // Wails' generated runtime is a thin wrapper over these, and EventsOn is
  // EventsOnMultiple with no limit - so this is the method to provide, not the
  // one the app appears to call.
  w.runtime = {
    EventsOnMultiple(name: string, handler: Handler, maxCallbacks: number) {
      let left = maxCallbacks;
      const set = listeners.get(name) ?? new Set<Handler>();
      const wrapped: Handler = (data) => {
        handler(data);
        if (left > 0) {
          left -= 1;
          if (left === 0) set.delete(wrapped);
        }
      };
      set.add(wrapped);
      listeners.set(name, set);
      return () => set.delete(wrapped);
    },
    EventsOff(...names: string[]) {
      for (const name of names) listeners.delete(name);
    },
    EventsOffAll() {
      listeners.clear();
    },
    EventsEmit: emit,
    LogPrint: console.log,
    WindowSetTitle() {},
    Quit() {},
  };
}
