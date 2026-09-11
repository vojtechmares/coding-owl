// The app's view of the daemon: the Wails bindings of internal/desktop, plus
// the polling and event hooks the views share. Nothing here knows the wire;
// every call is a method of the bound App (ADR-0009).

import { useCallback, useEffect, useRef, useState } from "react";
import * as App from "../../wailsjs/go/desktop/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import type { client, desktop } from "../../wailsjs/go/models";

export type Job = client.Job;
export type JobDetails = client.JobDetails;
export type Run = client.Run;
export type Project = client.Project;
export type Overview = client.Overview;
export type Status = desktop.Status;
export type StartResult = desktop.StartResult;

export const api = {
  status: (): Promise<Status> => App.Status(),
  projects: (): Promise<Project[]> => App.Projects(),
  overview: (): Promise<Overview> => App.Overview(),
  jobs: (all: boolean): Promise<Job[]> => App.Jobs(all),
  job: (id: number): Promise<JobDetails> => App.Job(id),
  start: (): Promise<StartResult> => App.Start(),
  accept: (id: number, force: boolean): Promise<Job> => App.Accept(id, force),
  drop: (id: number, force: boolean): Promise<Job> => App.Drop(id, force),
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

export function runOutcome(r: Run): string {
  return r.Outcome && r.Outcome !== "" ? r.Outcome : "running";
}
