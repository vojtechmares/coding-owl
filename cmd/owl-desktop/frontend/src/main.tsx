import React, { type ErrorInfo, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { install as installFake } from "./lib/fake";
import "./theme/tokens.css";
import "./style.css";

// A window with no daemon behind it, for looking at the design (#36). The flag
// is a constant at build time, so an ordinary build drops both the branch and
// the module it calls.
if (import.meta.env.VITE_FAKE) installFake(import.meta.env.VITE_FAKE);

// Boundary shows what went wrong instead of an empty window: a desktop app
// has no console a user would open.
class Boundary extends React.Component<{ children: ReactNode }, { error?: string }> {
  state = { error: undefined as string | undefined };
  static getDerivedStateFromError(err: unknown) {
    return { error: err instanceof Error ? `${err.message}\n${err.stack ?? ""}` : String(err) };
  }
  componentDidCatch(err: unknown, info: ErrorInfo) {
    console.error(err, info.componentStack);
  }
  render() {
    if (this.state.error) {
      return (
        <div className="content">
          <div className="banner alert">
            The app hit an error it could not recover from.
            <pre className="code">{this.state.error}</pre>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

function showFatal(msg: string) {
  const container = document.getElementById("root");
  if (!container) return;
  const pre = document.createElement("pre");
  pre.className = "code";
  pre.textContent = msg;
  container.replaceChildren(pre);
}

window.addEventListener("error", (e) => showFatal(`${e.message}\n${e.error?.stack ?? ""}`));
window.addEventListener("unhandledrejection", (e) => showFatal(`unhandled rejection: ${String(e.reason)}`));

const container = document.getElementById("root");
if (container) {
  createRoot(container).render(
    <React.StrictMode>
      <Boundary>
        <App />
      </Boundary>
    </React.StrictMode>,
  );
}
