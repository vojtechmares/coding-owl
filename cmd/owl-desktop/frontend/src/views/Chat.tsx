// Chat is the conversation the app holds with a model (ADR-0022). The daemon
// answers it, holds the keys and keeps the conversations; this view sends what
// the user typed and shows what comes back, piece by piece.

import { useEffect, useMemo, useRef, useState } from "react";
import {
  api,
  errorText,
  usePoll,
  useEvent,
  when,
  EVENT_CHAT_COMMAND,
  EVENT_CHAT_COMMAND_DONE,
  EVENT_CHAT_DELTA,
  EVENT_CHAT_END,
  type ChatCommand,
  type ChatCommandDone,
  type ChatDelta,
  type ChatEnd,
  type ChatMessage,
  type CommandDecision,
} from "../lib/api";
import { Banner, Button, Empty, Panel } from "../components/ui";

// line is argv as a person reads it. An argument with a space in it is quoted,
// because two words and one word with a space in it are different commands and
// somebody agreeing to one should not be shown the other.
const line = (argv: string[]): string =>
  argv.map((arg) => (arg === "" || /\s/.test(arg) ? JSON.stringify(arg) : arg)).join(" ");

// What a person may answer about a command the chat wants to run. Nothing runs
// until one of these is chosen, and refusing runs nothing at all (ADR-0022).
const DECISIONS: { decision: CommandDecision; label: string }[] = [
  { decision: "once", label: "Allow once" },
  { decision: "conversation", label: "Allow for this conversation" },
  { decision: "refuse", label: "Refuse" },
];

export function Chat() {
  const models = usePoll(() => api.models(), 10000);
  const conversations = usePoll(() => api.conversations(), 5000);
  // A model is chosen with the provider it comes from: two providers may
  // offer the same model (ADR-0022).
  const [chosenKey, setChosenKey] = useState<string | undefined>(undefined);
  const [open, setOpen] = useState<number | undefined>(undefined);
  const [said, setSaid] = useState<ChatMessage[]>([]);
  const [answer, setAnswer] = useState("");
  const [waiting, setWaiting] = useState(false);
  const [note, setNote] = useState<string | undefined>(undefined);
  const [text, setText] = useState("");
  // The commands waiting to be answered, and the ones that have run. Neither
  // is in the conversation the daemon keeps: what the model makes of them is.
  const [asking, setAsking] = useState<ChatCommand[]>([]);
  const [ran, setRan] = useState<ChatCommandDone[]>([]);
  const bottom = useRef<HTMLDivElement | null>(null);

  const offered = models.data ?? [];
  const chosen = offered.find((m) => `${m.Provider}/${m.ID}` === chosenKey) ?? offered[0];

  // What a command printed belongs to the conversation it ran in. The daemon
  // keeps what was said rather than what ran, so this is the only record of it
  // while the app is open, and it stays until another conversation is.
  useEffect(() => {
    setRan([]);
  }, [open]);

  // What was said in the conversation being read, which the daemon keeps.
  useEffect(() => {
    let alive = true;
    if (!open) {
      setSaid([]);
      return;
    }
    api
      .conversation(open)
      .then((d) => {
        if (alive) setSaid(d.Messages);
      })
      .catch((err) => {
        if (alive) setNote(errorText(err));
      });
    return () => {
      alive = false;
    };
  }, [open, waiting]);

  // A send the daemon refused before a conversation existed carries no id,
  // and zero is not one: reading it back would replace the failure the user
  // needs to see with "no conversation 0".
  useEvent<ChatDelta>(EVENT_CHAT_DELTA, (d) => {
    setOpen((current) => current ?? (d.conversationId || undefined));
    setAnswer((current) => current + d.text);
  });
  useEvent<ChatEnd>(EVENT_CHAT_END, (end) => {
    setWaiting(false);
    setAnswer("");
    setOpen((current) => current ?? (end.conversationId || undefined));
    if (end.error) setNote(end.error);
    // An exchange that ended leaves nothing to answer: a command nobody
    // answered in time is one the daemon has already refused for itself.
    setAsking([]);
    void conversations.refresh();
  });
  useEvent<ChatCommand>(EVENT_CHAT_COMMAND, (c) => {
    setOpen((current) => current ?? (c.conversationId || undefined));
    setAsking((current) => [...current, c]);
  });
  useEvent<ChatCommandDone>(EVENT_CHAT_COMMAND_DONE, (done) => {
    setRan((current) => [...current, done]);
  });

  // A command is answered once: it leaves the list whatever the answer was,
  // and the daemon runs it or does not.
  const answerCommand = async (c: ChatCommand, decision: CommandDecision) => {
    setAsking((current) => current.filter((waiting) => waiting.requestId !== c.requestId));
    try {
      await api.answerCommand(c.requestId, decision);
    } catch (err) {
      setNote(errorText(err));
    }
  };

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "end" });
  }, [said, answer, asking, ran]);

  const send = async () => {
    const asked = text.trim();
    if (!asked || !chosen) return;
    setText("");
    setNote(undefined);
    setWaiting(true);
    setSaid((current) => [...current, { Role: "user", Text: asked, Created: new Date().toISOString() } as ChatMessage]);
    try {
      const id = await api.sendTo(open ?? 0, chosen.Provider, chosen.ID, asked);
      if (id) setOpen(id);
    } catch (err) {
      setWaiting(false);
      setNote(errorText(err));
    }
  };

  const list = useMemo(() => conversations.data ?? [], [conversations.data]);

  return (
    <>
      <div className="page-title">
        <h1>Chat</h1>
        <div className="actions">
          <select
            value={chosen ? `${chosen.Provider}/${chosen.ID}` : ""}
            onChange={(e) => setChosenKey(e.target.value)}
            disabled={waiting}
          >
            {offered.map((m) => (
              <option key={`${m.Provider}/${m.ID}`} value={`${m.Provider}/${m.ID}`}>
                {m.ID} ({m.Provider})
              </option>
            ))}
          </select>
          <Button onClick={() => { setOpen(undefined); setSaid([]); setNote(undefined); }} disabled={waiting}>
            New conversation
          </Button>
        </div>
      </div>
      {models.error ? <Banner>{models.error}</Banner> : null}
      {note ? <Banner>{note}</Banner> : null}
      {offered.length === 0 ? (
        <Panel>
          <Empty>
            No model provider is configured. Configure one with owl providers add, and its key stays in the
            daemon.
          </Empty>
        </Panel>
      ) : (
        <div className="chat">
          <Panel title="Conversations">
            {list.length === 0 ? (
              <Empty>Nothing said yet.</Empty>
            ) : (
              <ul className="conversations">
                {list.map((c) => (
                  <li key={c.ID}>
                    <button className={c.ID === open ? "active" : ""} onClick={() => setOpen(c.ID)}>
                      <span>{c.Title || "(untitled)"}</span>
                      <span className="dim">{when(c.Updated)}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Panel>
          <Panel>
            <div className="messages">
              {said.length === 0 && !answer ? (
                <Empty>Ask about a Job, a Run, or what a check said.</Empty>
              ) : null}
              {said.map((m, at) => (
                <div key={at} className={`message ${m.Role}`}>
                  <pre className="code">{m.Text}</pre>
                </div>
              ))}
              {ran.map((done) => (
                <div key={done.requestId} className="message command">
                  <div className="dim">
                    {line(done.argv)} in {done.directory} - exit {done.exitCode}
                  </div>
                  <pre className="code">
                    {done.output || "(it printed nothing)"}
                    {done.cut ? "\n(this is the beginning of what it printed; there was more)" : ""}
                  </pre>
                </div>
              ))}
              {asking.map((c) => (
                <div key={c.requestId} className="message command asking">
                  <div>
                    The chat would like to run <code>{line(c.argv)}</code> in <code>{c.directory}</code>.
                  </div>
                  <div className="actions">
                    {DECISIONS.map(({ decision, label }) => (
                      <Button
                        key={decision}
                        kind={decision === "refuse" ? undefined : "primary"}
                        onClick={() => void answerCommand(c, decision)}
                      >
                        {label}
                      </Button>
                    ))}
                  </div>
                </div>
              ))}
              {answer ? (
                <div className="message assistant">
                  <pre className="code">{answer}</pre>
                </div>
              ) : null}
              <div ref={bottom} />
            </div>
            <div className="ask">
              <textarea
                value={text}
                placeholder="Why was job 7 blocked?"
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) void send();
                }}
                disabled={waiting}
              />
              <Button kind="primary" onClick={() => void send()} disabled={waiting || !text.trim()}>
                {waiting ? "Answering" : "Send"}
              </Button>
            </div>
          </Panel>
        </div>
      )}
    </>
  );
}
