// Skills is owl skills list for one Project, with the update the CLI does:
// the Skills a Project gives every Agent that works in it (ADR-0024).

import { useState } from "react";
import { api, errorText, orNone, usePoll } from "../lib/api";
import { Banner, Button, Empty, Panel } from "../components/ui";

// short is a commit as a listing shows it, the way the CLI shortens it.
function short(commit: string): string {
  return commit.length > 12 ? commit.slice(0, 12) : commit;
}

export function Skills() {
  const projects = usePoll(() => api.projects(), 5000);
  const [chosen, setChosen] = useState<string | undefined>(undefined);
  const project = chosen ?? projects.data?.[0]?.Name;
  const skills = usePoll(() => (project ? api.skills(project) : Promise.resolve([])), 3000, [project]);
  const [note, setNote] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);

  const update = async (name?: string) => {
    if (!project) return;
    setBusy(true);
    setNote(undefined);
    try {
      const r = await api.updateSkills(project, name ? [name] : []);
      const moved = r.updated ?? [];
      setNote(
        moved.length === 0
          ? "Nothing moved: every skill is already at what its ref resolves to"
          : `Updated ${moved.map((s) => `${s.Name} to ${short(s.Commit)}`).join(", ")}` +
              (r.files?.InRepo ? `. Commit ${r.files.Manifest} and ${r.files.Lock}.` : ""),
      );
    } catch (err) {
      setNote(errorText(err));
    } finally {
      setBusy(false);
      await skills.refresh();
    }
  };

  return (
    <>
      <div className="page-title">
        <h1>Skills</h1>
        <div className="actions">
          <select value={project ?? ""} onChange={(e) => setChosen(e.target.value)} disabled={busy}>
            {(projects.data ?? []).map((p) => (
              <option key={p.Name} value={p.Name}>
                {p.Name}
              </option>
            ))}
          </select>
          <Button kind="primary" onClick={() => update()} disabled={busy || !project}>
            Update tracking
          </Button>
        </div>
      </div>
      {projects.error ? <Banner>{projects.error}</Banner> : null}
      {skills.error ? <Banner>{skills.error}</Banner> : null}
      {note ? <Banner>{note}</Banner> : null}
      <Panel>
        {!project ? (
          <Empty>No Project registered. Register one with owl project add.</Empty>
        ) : !skills.data || skills.data.length === 0 ? (
          <Empty>This Project declares no Skills. Add one with owl skills add.</Empty>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Source</th>
                <th>Ref</th>
                <th>Commit</th>
                <th>Moves</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {skills.data.map((s) => (
                <tr key={s.Name} className="row">
                  <td>{s.Name}</td>
                  <td className="mono dim">{s.Source}</td>
                  <td className="mono">{orNone(s.Ref)}</td>
                  <td className="mono">{short(s.Commit)}</td>
                  <td>
                    <span className={`pill ${s.AutoUpdate ? "tracking" : "pinned"}`}>
                      {s.AutoUpdate ? "tracking" : "pinned"}
                    </span>
                  </td>
                  <td>
                    <Button onClick={() => update(s.Name)} disabled={busy}>
                      Update
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </>
  );
}
