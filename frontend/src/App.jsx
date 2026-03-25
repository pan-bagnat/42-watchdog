import { useState } from "react";

const defaultEndpoint = "/api/admin/commands";

const commandPresets = [
  { label: "Start Listening", command: "start_listen", parameters: null },
  { label: "Get Status", command: "get_status", parameters: null },
  { label: "Notify Students", command: "notify_students", parameters: null },
  { label: "Stop Listening", command: "stop_listen", parameters: {} },
  {
    label: "Stop + Post Attendances",
    command: "stop_listen",
    parameters: { post_attendance: true }
  }
];

function CommandButton({ preset, onRun, busy }) {
  return (
    <button className="action-tile" onClick={() => onRun(preset)} disabled={busy}>
      <span>{preset.label}</span>
      <code>{preset.command}</code>
    </button>
  );
}

function App() {
  const [endpoint, setEndpoint] = useState(defaultEndpoint);
  const [authorization, setAuthorization] = useState("");
  const [sessionId, setSessionId] = useState("");
  const [manualCommand, setManualCommand] = useState("update_student_status");
  const [manualParameters, setManualParameters] = useState('{"login":"student","is_alternant":true}');
  const [busy, setBusy] = useState(false);
  const [response, setResponse] = useState("No request sent yet.");
  const [requestPreview, setRequestPreview] = useState("");

  async function sendCommand(payload) {
    setBusy(true);
    setRequestPreview(JSON.stringify(payload, null, 2));

    try {
      const headers = {
        "Content-Type": "application/json"
      };
      if (authorization.trim() !== "") {
        headers.Authorization = authorization.trim();
      }
      if (sessionId.trim() !== "") {
        headers["X-Session-Id"] = sessionId.trim();
      }

      const res = await fetch(endpoint, {
        method: "POST",
        headers,
        body: JSON.stringify(payload)
      });

      const body = await res.text();
      setResponse(`HTTP ${res.status} ${res.statusText}\n\n${body}`);
    } catch (error) {
      setResponse(`Request failed\n\n${error instanceof Error ? error.message : String(error)}`);
    } finally {
      setBusy(false);
    }
  }

  function runManualCommand() {
    let parsed = undefined;
    const trimmed = manualParameters.trim();
    if (trimmed !== "") {
      try {
        parsed = JSON.parse(trimmed);
      } catch (error) {
        setResponse(`Manual parameters are not valid JSON\n\n${error instanceof Error ? error.message : String(error)}`);
        return;
      }
    }
    void sendCommand({
      command: manualCommand.trim(),
      parameters: parsed
    });
  }

  return (
    <main className="shell">
      <section className="hero">
        <div>
          <p className="eyebrow">42 Watchdog</p>
          <h1>Attendance control room</h1>
          <p className="lede">
            This frontend sends commands through nginx to the Go watchdog backend. Remote commands
            still require Panbagnat-backed authentication on the backend side.
          </p>
        </div>
        <div className="hero-card">
          <div className="metric">
            <span>Public entrypoint</span>
            <strong>/</strong>
          </div>
          <div className="metric">
            <span>Admin route</span>
            <strong>/api/admin/commands</strong>
          </div>
          <div className="metric">
            <span>Student route</span>
            <strong>/api/student/me</strong>
          </div>
          <div className="metric">
            <span>Webhook route</span>
            <strong>/webhook/access-control</strong>
          </div>
        </div>
      </section>

      <section className="panel">
        <div className="panel-header">
          <h2>Connection</h2>
          <p>Use same-origin nginx routing by default, or override the endpoint if needed.</p>
        </div>
        <div className="grid two">
          <label>
            <span>Command endpoint</span>
            <input value={endpoint} onChange={(event) => setEndpoint(event.target.value)} />
          </label>
          <label>
            <span>Authorization header</span>
            <input
              value={authorization}
              onChange={(event) => setAuthorization(event.target.value)}
              placeholder="Bearer ..."
            />
          </label>
          <label>
            <span>X-Session-Id</span>
            <input
              value={sessionId}
              onChange={(event) => setSessionId(event.target.value)}
              placeholder="Optional session id"
            />
          </label>
          <div className="hint-card">
            <strong>Note</strong>
            <p>
              Browsers cannot manually set the raw <code>Cookie</code> header. This UI therefore
              supports <code>Authorization</code> and <code>X-Session-Id</code>.
            </p>
          </div>
        </div>
      </section>

      <section className="panel">
        <div className="panel-header">
          <h2>Quick commands</h2>
          <p>Fast buttons for the most common server actions.</p>
        </div>
        <div className="actions">
          {commandPresets.map((preset) => (
            <CommandButton
              key={preset.label}
              preset={preset}
              onRun={(value) => void sendCommand(value)}
              busy={busy}
            />
          ))}
        </div>
      </section>

      <section className="panel">
        <div className="panel-header">
          <h2>Manual payload</h2>
          <p>Send any command body accepted by the watchdog backend.</p>
        </div>
        <div className="grid two">
          <label>
            <span>Command</span>
            <input value={manualCommand} onChange={(event) => setManualCommand(event.target.value)} />
          </label>
          <div className="spacer" />
          <label className="full">
            <span>Parameters JSON</span>
            <textarea
              rows={8}
              value={manualParameters}
              onChange={(event) => setManualParameters(event.target.value)}
            />
          </label>
        </div>
        <button className="primary" onClick={runManualCommand} disabled={busy || manualCommand.trim() === ""}>
          {busy ? "Sending..." : "Send manual command"}
        </button>
      </section>

      <section className="panel outputs">
        <div>
          <div className="panel-header">
            <h2>Request</h2>
          </div>
          <pre>{requestPreview || "No payload prepared yet."}</pre>
        </div>
        <div>
          <div className="panel-header">
            <h2>Response</h2>
          </div>
          <pre>{response}</pre>
        </div>
      </section>
    </main>
  );
}

export default App;
