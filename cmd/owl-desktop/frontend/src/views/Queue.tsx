// Queue is owl queue list: the pending Jobs in the order they will run.

import { useState } from "react";
import { api, errorText, usePoll } from "../lib/api";
import { Banner, Button, Empty, Panel } from "../components/ui";
import { JobsTable } from "./JobsTable";

export function Queue({ onOpen }: { onOpen: (id: number) => void }) {
  const { data, error, refresh } = usePoll(() => api.jobs(false), 2000);
  const [note, setNote] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);

  const start = async () => {
    setBusy(true);
    try {
      const r = await api.start();
      setNote(r.started ? `Started run ${r.run.ID} for job ${r.job.ID}` : "Nothing pending to run");
    } catch (err) {
      setNote(errorText(err));
    } finally {
      setBusy(false);
      await refresh();
    }
  };

  return (
    <>
      <div className="page-title">
        <h1>Queue</h1>
        <div className="actions">
          <Button kind="primary" onClick={start} disabled={busy}>
            Start next
          </Button>
        </div>
      </div>
      {error ? <Banner alert>{error}</Banner> : null}
      {note ? <Banner>{note}</Banner> : null}
      <Panel>
        {!data || data.length === 0 ? (
          <Empty>The queue is empty. Queue a Job with owl add.</Empty>
        ) : (
          <JobsTable jobs={data} onOpen={onOpen} position />
        )}
      </Panel>
    </>
  );
}
