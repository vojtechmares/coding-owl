// Projects is owl project list.

import { api, usePoll, when } from "../lib/api";
import { Banner, Empty, Panel } from "../components/ui";

export function Projects() {
  const { data, error } = usePoll(() => api.projects(), 3000);
  return (
    <>
      <div className="page-title">
        <h1>Projects</h1>
      </div>
      {error ? <Banner alert>{error}</Banner> : null}
      <Panel>
        {!data || data.length === 0 ? (
          <Empty>No Project registered. Register one with owl project add.</Empty>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Path</th>
                <th>Base branch</th>
                <th>Registered</th>
              </tr>
            </thead>
            <tbody>
              {data.map((p) => (
                <tr key={p.Name} className="row">
                  <td>{p.Name}</td>
                  <td className="mono dim">{p.Path}</td>
                  <td className="mono">{p.BaseBranch}</td>
                  <td className="dim">{when(p.Registered)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </>
  );
}
