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
          {position ? <th className="tight">#</th> : null}
          <th className="tight">Job</th>
          <th className="tight">Project</th>
          <th className="tight">State</th>
          <th>Prompt</th>
          {reason ? <th>Reason</th> : null}
          <th className="tight">Branch</th>
          <th className="tight">Attempts</th>
          <th className="tight">Created</th>
          {actions ? <th /> : null}
        </tr>
      </thead>
      <tbody>
        {jobs.map((j) => (
          <tr key={j.ID} className="row" onClick={() => onOpen(j.ID)}>
            {position ? <td className="mono tight">{j.Position}</td> : null}
            <td className="mono tight">{j.ID}</td>
            <td className="tight">{j.Project}</td>
            <td className="tight">
              <StatePill state={j.State} />
            </td>
            <td className="prompt" title={j.Prompt}>
              {j.Prompt}
            </td>
            {reason ? (
              <td className="dim reason" title={j.Reason}>
                {j.Reason}
              </td>
            ) : null}
            <td className="mono dim tight">{j.Branch || "-"}</td>
            <td className="dim tight">{j.TTL} left</td>
            <td className="dim tight">{ago(j.Created)}</td>
            {actions ? <td onClick={(e) => e.stopPropagation()}>{actions(j)}</td> : null}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
