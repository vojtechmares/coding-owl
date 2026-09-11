// The app's view of the daemon: the Wails bindings of internal/desktop, plus
// the polling and event hooks the views share. Nothing here knows the wire;
// every call is a method of the bound App (ADR-0009).

import { useCallback, useEffect, useRef, useState } from "react";
import * as App from "../../wailsjs/go/desktop/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { client, type desktop } from "../../wailsjs/go/models";

export type Job = client.Job;
export type JobDetails = client.JobDetails;
export type Run = client.Run;
export type Project = client.Project;
export type Overview = client.Overview;
export type Status = desktop.Status;
export type StartResult = desktop.StartResult;
export type Skill = client.Skill;
export type SkillUpdate = desktop.SkillUpdate;

// Go marshals a nil slice as null, so every list that crosses the bindings
// is made an array here, once, and the views never have to ask.
function list<T>(v: T[] | null | undefined): T[] {
  return v ?? [];
}

function overview(o: Overview): Overview {
  o.Running = list(o.Running);
  o.Counts = list(o.Counts);
  o.Awaiting = list(o.Awaiting);
  o.Blocked = list(o.Blocked);
  o.Exhausted = list(o.Exhausted);
  o.Unfinished = list(o.Unfinished);
  return o;
}

function details(d: JobDetails): JobDetails {
  d.Runs = list(d.Runs);
  d.Phases = list(d.Phases);
  d.Checks = list(d.Checks);
  if (!d.Diff) d.Diff = new client.DiffSummary({ Files: [], Insertions: 0, Deletions: 0 });
  d.Diff.Files = list(d.Diff.Files);
  return d;
}

export const api = {
  status: (): Promise<Status> => App.Status(),
  projects: (): Promise<Project[]> => App.Projects().then(list),
  overview: (): Promise<Overview> => App.Overview().then(overview),
  jobs: (all: boolean): Promise<Job[]> => App.Jobs(all).then(list),
  job: (id: number): Promise<JobDetails> => App.Job(id).then(details),
  start: (): Promise<StartResult> => App.Start(),
  pause: (): Promise<Run> => App.Pause(),
  resume: (): Promise<Run> => App.Resume(),
  accept: (id: number, force: boolean): Promise<Job> => App.Accept(id, force),
  drop: (id: number, force: boolean): Promise<Job> => App.Drop(id, force),
  skills: (project: string): Promise<Skill[]> => App.Skills(project).then(list),
  updateSkills: (project: string, names: string[]): Promise<SkillUpdate> =>
    App.UpdateSkills(project, names).then((u) => {
      u.updated = list(u.updated);
      u.all = list(u.all);
      return u;
    }),
  followLog: (runId: number): Promise<void> => App.FollowLog(runId),
  stopLog: (runId: number): Promise<void> => App.StopLog(runId),
};

// Events the Go side emits, named as internal/desktop names them.
export const EVENT_LOG_LINE = "run:log";
export const EVENT_LOG_END = "run:log:end";

export interface LogLine {
  runId: number;
  line: string;
}

export interface LogEnd {
  runId: number;
  error: string;
}

// errorText turns whatever a rejected binding carried into something to show.
export function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return String(err);
}

// usePoll calls load every intervalMs, and again on demand, keeping the last
// good value while a call is in flight and the last error otherwise. Polling
// is how the app stays live: the daemon has no subscription API, and the CLI
// asks the same way.
export function usePoll<T>(load: () => Promise<T>, intervalMs: number, deps: unknown[] = []) {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const alive = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const v = await load();
      if (!alive.current) return;
      setData(v);
      setError(undefined);
    } catch (err) {
      if (!alive.current) return;
      setError(errorText(err));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    alive.current = true;
    void refresh();
    const t = setInterval(() => void refresh(), intervalMs);
    return () => {
      alive.current = false;
      clearInterval(t);
    };
  }, [refresh, intervalMs]);

  return { data, error, refresh };
}

// useEvent subscribes to one Wails event for the life of the component.
export function useEvent<T>(name: string, handler: (data: T) => void) {
  const latest = useRef(handler);
  latest.current = handler;
  useEffect(() => {
    const off = EventsOn(name, (data: T) => latest.current(data));
    return () => off();
  }, [name]);
}

// Formatting helpers shared by the views.

export function when(v: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return "";
  return d.toLocaleString();
}

export function ago(v: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return "";
  const s = Math.max(0, Math.round((Date.now() - d.getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

export function orNone(s: string | undefined): string {
  return s && s !== "" ? s : "(none)";
}

// runOutcome is what a Run is doing or how it ended: a frozen Run has not
// ended, and says so (ADR-0011).
export function runOutcome(r: Run): string {
  if (r.Outcome && r.Outcome !== "") return r.Outcome;
  return r.Paused ? "paused" : "running";
}

// runPill is the pill for a Run's state: done for one that succeeded, paused
// for one that is frozen, active for one still going, blocked otherwise.
export function runPill(r: Run): string {
  const o = runOutcome(r);
  if (o === "succeeded") return "done";
  if (o === "paused") return "paused";
  if (o === "running") return "active";
  return "blocked";
}
