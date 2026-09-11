// Jobs is owl jobs list --all: every Job whatever its state.

import { api, usePoll } from "../lib/api";
import { Banner, Empty, Panel } from "../components/ui";
import { JobsTable } from "./JobsTable";

export function Jobs({ onOpen }: { onOpen: (id: number) => void }) {
  const { data, error } = usePoll(() => api.jobs(true), 2000);
  return (
    <>
      <div className="page-title">
        <h1>Jobs</h1>
      </div>
      {error ? <Banner>{error}</Banner> : null}
      <Panel>
        {!data || data.length === 0 ? <Empty>No Job yet.</Empty> : <JobsTable jobs={data} onOpen={onOpen} reason />}
      </Panel>
    </>
  );
}
