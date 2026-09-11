// JobsTable renders Jobs the way owl queue list and owl jobs list do, with
// an optional actions column.

import type { ReactNode } from "react";
import { ago, type Job } from "../lib/api";
import { StatePill } from "../components/ui";

export function JobsTable({
  jobs,
  onOpen,
  actions,
  position,
  reason,
}: {
  jobs: Job[];
  onOpen: (id: number) => void;
  actions?: (j: Job) => ReactNode;
  position?: boolean;
  reason?: boolean;
}) {
  return (
    <table>
      <thead>
        <tr>
          {position ? <th>#</th> : null}
          <th>Job</th>
          <th>Project</th>
          <th>State</th>
          <th>Prompt</th>
          {reason ? <th>Reason</th> : null}
          <th>Branch</th>
          <th>Attempts</th>
          <th>Created</th>
          {actions ? <th /> : null}
        </tr>
      </thead>
      <tbody>
        {jobs.map((j) => (
          <tr key={j.ID} className="row" onClick={() => onOpen(j.ID)}>
            {position ? <td className="mono">{j.Position}</td> : null}
            <td className="mono">{j.ID}</td>
            <td>{j.Project}</td>
            <td>
              <StatePill state={j.State} />
            </td>
            <td className="prompt" title={j.Prompt}>
              {j.Prompt}
            </td>
            {reason ? <td className="dim">{j.Reason}</td> : null}
            <td className="mono dim">{j.Branch || "-"}</td>
            <td className="dim">{j.TTL} left</td>
            <td className="dim">{ago(j.Created)}</td>
            {actions ? <td onClick={(e) => e.stopPropagation()}>{actions(j)}</td> : null}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
