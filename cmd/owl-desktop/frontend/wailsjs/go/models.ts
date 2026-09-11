export namespace client {
	
	export class ChatMessage {
	    Role: string;
	    Text: string;
	    // Go type: time
	    Created: any;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Role = source["Role"];
	        this.Text = source["Text"];
	        this.Created = this.convertValues(source["Created"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChatModel {
	    Provider: string;
	    ID: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.ID = source["ID"];
	    }
	}
	export class CheckResult {
	    Name: string;
	    Command: string;
	    Passed: boolean;
	    ExitCode: number;
	    Output: string;
	    Reason: string;
	    Verifier: string;
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Command = source["Command"];
	        this.Passed = source["Passed"];
	        this.ExitCode = source["ExitCode"];
	        this.Output = source["Output"];
	        this.Reason = source["Reason"];
	        this.Verifier = source["Verifier"];
	    }
	}
	export class Conversation {
	    ID: number;
	    Title: string;
	    Model: string;
	    // Go type: time
	    Created: any;
	    // Go type: time
	    Updated: any;
	
	    static createFrom(source: any = {}) {
	        return new Conversation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Title = source["Title"];
	        this.Model = source["Model"];
	        this.Created = this.convertValues(source["Created"], null);
	        this.Updated = this.convertValues(source["Updated"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConversationDetails {
	    Conversation: Conversation;
	    Messages: ChatMessage[];
	
	    static createFrom(source: any = {}) {
	        return new ConversationDetails(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Conversation = this.convertValues(source["Conversation"], Conversation);
	        this.Messages = this.convertValues(source["Messages"], ChatMessage);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiffFile {
	    Path: string;
	    Insertions: number;
	    Deletions: number;
	
	    static createFrom(source: any = {}) {
	        return new DiffFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Insertions = source["Insertions"];
	        this.Deletions = source["Deletions"];
	    }
	}
	export class DiffSummary {
	    Files: DiffFile[];
	    Insertions: number;
	    Deletions: number;
	
	    static createFrom(source: any = {}) {
	        return new DiffSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Files = this.convertValues(source["Files"], DiffFile);
	        this.Insertions = source["Insertions"];
	        this.Deletions = source["Deletions"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Job {
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
	    // Go type: time
	    Created: any;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Source = source["Source"];
	        this.SourceRef = source["SourceRef"];
	        this.Project = source["Project"];
	        this.Prompt = source["Prompt"];
	        this.State = source["State"];
	        this.Branch = source["Branch"];
	        this.Worktree = source["Worktree"];
	        this.Planned = source["Planned"];
	        this.Plan = source["Plan"];
	        this.Reason = source["Reason"];
	        this.TTL = source["TTL"];
	        this.Account = source["Account"];
	        this.Position = source["Position"];
	        this.Created = this.convertValues(source["Created"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PhaseSettings {
	    Phase: string;
	    Model: string;
	    ModelFrom: string;
	    Effort: string;
	    EffortFrom: string;
	
	    static createFrom(source: any = {}) {
	        return new PhaseSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Phase = source["Phase"];
	        this.Model = source["Model"];
	        this.ModelFrom = source["ModelFrom"];
	        this.Effort = source["Effort"];
	        this.EffortFrom = source["EffortFrom"];
	    }
	}
	export class Skill {
	    Name: string;
	    Source: string;
	    Ref: string;
	    Commit: string;
	    Digest: string;
	    AutoUpdate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Skill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Source = source["Source"];
	        this.Ref = source["Ref"];
	        this.Commit = source["Commit"];
	        this.Digest = source["Digest"];
	        this.AutoUpdate = source["AutoUpdate"];
	    }
	}
	export class Run {
	    ID: number;
	    JobID: number;
	    Attempt: number;
	    // Go type: time
	    Started: any;
	    // Go type: time
	    Ended: any;
	    Outcome: string;
	    Error: string;
	    ExitCode: number;
	    LogPath: string;
	    Phase: string;
	    Skills: Skill[];
	    Paused: boolean;
	    Stage: string;
	
	    static createFrom(source: any = {}) {
	        return new Run(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.JobID = source["JobID"];
	        this.Attempt = source["Attempt"];
	        this.Started = this.convertValues(source["Started"], null);
	        this.Ended = this.convertValues(source["Ended"], null);
	        this.Outcome = source["Outcome"];
	        this.Error = source["Error"];
	        this.ExitCode = source["ExitCode"];
	        this.LogPath = source["LogPath"];
	        this.Phase = source["Phase"];
	        this.Skills = this.convertValues(source["Skills"], Skill);
	        this.Paused = source["Paused"];
	        this.Stage = source["Stage"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobDetails {
	    Job: Job;
	    Runs: Run[];
	    SystemPrompt: string;
	    VerifierSystemPrompt: string;
	    Phases: PhaseSettings[];
	    Checks: CheckResult[];
	    Handoff: string;
	    Diff: DiffSummary;
	
	    static createFrom(source: any = {}) {
	        return new JobDetails(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Job = this.convertValues(source["Job"], Job);
	        this.Runs = this.convertValues(source["Runs"], Run);
	        this.SystemPrompt = source["SystemPrompt"];
	        this.VerifierSystemPrompt = source["VerifierSystemPrompt"];
	        this.Phases = this.convertValues(source["Phases"], PhaseSettings);
	        this.Checks = this.convertValues(source["Checks"], CheckResult);
	        this.Handoff = source["Handoff"];
	        this.Diff = this.convertValues(source["Diff"], DiffSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Machine {
	    Read: boolean;
	    Idle: boolean;
	    Since: number;
	    OnPower: boolean;
	    Detail: string;
	
	    static createFrom(source: any = {}) {
	        return new Machine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Read = source["Read"];
	        this.Idle = source["Idle"];
	        this.Since = source["Since"];
	        this.OnPower = source["OnPower"];
	        this.Detail = source["Detail"];
	    }
	}
	export class Unfinished {
	    Job: number;
	    Project: string;
	    Path: string;
	    Reason: string;
	    Since: number;
	
	    static createFrom(source: any = {}) {
	        return new Unfinished(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Job = source["Job"];
	        this.Project = source["Project"];
	        this.Path = source["Path"];
	        this.Reason = source["Reason"];
	        this.Since = source["Since"];
	    }
	}
	export class StateCount {
	    State: string;
	    Count: number;
	
	    static createFrom(source: any = {}) {
	        return new StateCount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.State = source["State"];
	        this.Count = source["Count"];
	    }
	}
	export class RunInProgress {
	    Run: Run;
	    Job: Job;
	
	    static createFrom(source: any = {}) {
	        return new RunInProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Run = this.convertValues(source["Run"], Run);
	        this.Job = this.convertValues(source["Job"], Job);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Overview {
	    Running: RunInProgress[];
	    Counts: StateCount[];
	    Awaiting: Job[];
	    Blocked: Job[];
	    Exhausted: Job[];
	    Unfinished: Unfinished[];
	    Machine: Machine;
	    Holding: string;
	
	    static createFrom(source: any = {}) {
	        return new Overview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Running = this.convertValues(source["Running"], RunInProgress);
	        this.Counts = this.convertValues(source["Counts"], StateCount);
	        this.Awaiting = this.convertValues(source["Awaiting"], Job);
	        this.Blocked = this.convertValues(source["Blocked"], Job);
	        this.Exhausted = this.convertValues(source["Exhausted"], Job);
	        this.Unfinished = this.convertValues(source["Unfinished"], Unfinished);
	        this.Machine = this.convertValues(source["Machine"], Machine);
	        this.Holding = source["Holding"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Project {
	    Name: string;
	    Path: string;
	    BaseBranch: string;
	    // Go type: time
	    Registered: any;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Path = source["Path"];
	        this.BaseBranch = source["BaseBranch"];
	        this.Registered = this.convertValues(source["Registered"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class SkillFiles {
	    Manifest: string;
	    Lock: string;
	    InRepo: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillFiles(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Manifest = source["Manifest"];
	        this.Lock = source["Lock"];
	        this.InRepo = source["InRepo"];
	    }
	}
	

}

export namespace desktop {
	
	export class SkillUpdate {
	    updated: client.Skill[];
	    all: client.Skill[];
	    files: client.SkillFiles;
	
	    static createFrom(source: any = {}) {
	        return new SkillUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.updated = this.convertValues(source["updated"], client.Skill);
	        this.all = this.convertValues(source["all"], client.Skill);
	        this.files = this.convertValues(source["files"], client.SkillFiles);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StartResult {
	    started: boolean;
	    job: client.Job;
	    run: client.Run;
	
	    static createFrom(source: any = {}) {
	        return new StartResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.started = source["started"];
	        this.job = this.convertValues(source["job"], client.Job);
	        this.run = this.convertValues(source["run"], client.Run);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Status {
	    running: boolean;
	    version: string;
	    uptime: string;
	    socketPath: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.version = source["version"];
	        this.uptime = source["uptime"];
	        this.socketPath = source["socketPath"];
	        this.error = source["error"];
	    }
	}

}

