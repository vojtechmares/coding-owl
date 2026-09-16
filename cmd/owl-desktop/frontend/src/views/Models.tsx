// Models is what a Job's phases can run on: the models the daemon's Driver
// serves, as `owl models` lists them (ADR-0028). It is a reference rather than
// a control - a phase's model is set in a Project's configuration file, which
// a Run reads from the base branch (ADR-0014), so there is nothing to pick
// here.

import { api, errorText, usePoll } from "../lib/api";
import { Banner, Empty, Panel } from "../components/ui";

export function Models() {
  const models = usePoll(() => api.driverModels(), 30000);
  const driver = models.data?.Driver;
  const rows = models.data?.Models ?? [];

  return (
    <>
      <div className="page-title">
        <h1>Models</h1>
      </div>
      {models.error ? <Banner alert>{errorText(models.error)}</Banner> : null}
      <Panel>
        {rows.length === 0 ? (
          <Empty>Nothing to show yet. The daemon reports the models its Driver serves.</Empty>
        ) : (
          <>
            <p className="dim">
              Set one per phase in a Project's <span className="mono">.coding-owl.yaml</span>. An alias
              follows whatever the vendor currently calls its latest of that model; a pinned name stays
              where it is.
            </p>
            <table>
              <thead>
                <tr>
                  <th>Model</th>
                  <th>Kind</th>
                  <th>About</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((m) => (
                  <tr key={m.Name} className="row">
                    <td className="mono">{m.Name}</td>
                    <td>
                      <span className={`pill ${m.Alias ? "tracking" : "pinned"}`}>
                        {m.Alias ? "alias" : "pinned"}
                      </span>
                    </td>
                    <td className="dim">{m.About}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {driver ? <p className="dim">Served by {driver}.</p> : null}
          </>
        )}
      </Panel>
    </>
  );
}
