// Overview is owl status: what is running, how the Jobs stand, and what is
// waiting for a decision.

import { useState } from "react";
import { api, ago, errorText, type Job, type Overview as OverviewData } from "../lib/api";
import { Banner, Button, Empty, Panel, Tile } from "../components/ui";
import { JobsTable } from "./JobsTable";

export function Overview({
  data,
  error,
  refresh,
  onOpen,
}: {
  data: OverviewData | undefined;
  error: string | undefined;
  refresh: () => Promise<void>;
  onOpen: (id: number) => void;
}) {
  const [note, setNote] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);

  const act = async (what: () => Promise<unknown>, done: (r: unknown) => string) => {
    setBusy(true);
    try {
      const r = await what();
      setNote(done(r));
    } catch (err) {
      setNote(errorText(err));
    } finally {
      setBusy(false);
      await refresh();
    }
  };

  const start = () =>
    act(
      () => api.start(),
      (r) => {
        const res = r as { started: boolean; job: Job; run: { ID: number } };
        return res.started ? `Started run ${res.run.ID} for job ${res.job.ID}` : "Nothing pending to run";
      },
    );
  const accept = (j: Job) => act(() => api.accept(j.ID, false), () => `Accepted job ${j.ID}`);
  const drop = (j: Job) => act(() => api.drop(j.ID, false), () => `Dropped job ${j.ID}`);

  const counts = data?.Counts ?? [];
  const empty = data && data.Running.length === 0 && counts.length === 0 && data.Unfinished.length === 0;

  return (
    <>
      <div className="page-title">
        <h1>Overview</h1>
        <div className="actions">
          <Button kind="primary" onClick={start} disabled={busy}>
            Start next
          </Button>
        </div>
      </div>
      {error ? <Banner>{error}</Banner> : null}
      {note ? <Banner>{note}</Banner> : null}

      <Panel title="Running">
        {!data || data.Running.length === 0 ? (
          <Empty>No Run in progress.</Empty>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Run</th>
                <th>Job</th>
                <th>Project</th>
                <th>Phase</th>
                <th>Attempt</th>
                <th>Started</th>
                <th>Prompt</th>
              </tr>
            </thead>
            <tbody>
              {data.Running.map((r) => (
                <tr key={r.Run.ID} className="row" onClick={() => onOpen(r.Job.ID)}>
                  <td className="mono">{r.Run.ID}</td>
                  <td className="mono">{r.Job.ID}</td>
                  <td>{r.Job.Project}</td>
                  <td>{r.Run.Phase || "-"}</td>
                  <td>{r.Run.Attempt}</td>
                  <td className="dim">{ago(r.Run.Started)}</td>
                  <td className="prompt">{r.Job.Prompt}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Jobs">
        {empty ? (
          <Empty>Nothing queued yet. Register a Project and queue a Job with owl add.</Empty>
        ) : (
          <div className="tiles">
            {counts.map((c) => (
              <Tile key={c.State} label={c.State} value={c.Count} />
            ))}
          </div>
        )}
      </Panel>

      {data && data.Awaiting.length > 0 ? (
        <Panel title="Awaiting a decision">
          <JobsTable
            jobs={data.Awaiting}
            onOpen={onOpen}
            actions={(j) => (
              <div className="btn-row">
                <Button onClick={() => accept(j)} disabled={busy}>
                  Accept
                </Button>
                <Button kind="danger" onClick={() => drop(j)} disabled={busy}>
                  Drop
                </Button>
              </div>
            )}
          />
        </Panel>
      ) : null}

      {data && data.Blocked.length > 0 ? (
        <Panel title="Blocked">
          <JobsTable jobs={data.Blocked} onOpen={onOpen} reason />
        </Panel>
      ) : null}

      {data && data.Exhausted.length > 0 ? (
        <Panel title="Exhausted">
          <JobsTable jobs={data.Exhausted} onOpen={onOpen} reason />
        </Panel>
      ) : null}

      {data && data.Unfinished.length > 0 ? (
        <Panel title="Unfinished work found by garbage collection">
          <table>
            <thead>
              <tr>
                <th>Job</th>
                <th>Project</th>
                <th>Path</th>
                <th>Why it was left</th>
              </tr>
            </thead>
            <tbody>
              {data.Unfinished.map((u) => (
                <tr key={u.Path} className="row" onClick={() => u.Job && onOpen(u.Job)}>
                  <td className="mono">{u.Job || "-"}</td>
                  <td>{u.Project}</td>
                  <td className="mono">{u.Path}</td>
                  <td className="dim">{u.Reason}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>
      ) : null}
    </>
  );
}
