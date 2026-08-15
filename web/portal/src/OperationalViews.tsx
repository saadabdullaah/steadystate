type RecordValue = Record<string, any>;

function text(value: unknown, fallback = "—") {
  if (value === undefined || value === null || value === "") return fallback;
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function tone(value: unknown) {
  const normalized = text(value, "unknown").toLowerCase();
  if (/healthy|ready|true|success|passed|completed|running/.test(normalized)) return "good";
  if (/degraded|failed|false|error|denied|aborted/.test(normalized)) return "bad";
  return "warn";
}

function Signal({ label, value, detail }: { label: string; value: unknown; detail?: string }) {
  return <article className="signal"><span>{label}</span><strong>{text(value)}</strong>{detail && <small>{detail}</small>}</article>;
}

function State({ value }: { value: unknown }) {
  return <span className={`state state-${tone(value)}`}><i aria-hidden="true" />{text(value, "Unknown")}</span>;
}

function EmptyState({ title, detail }: { title: string; detail: string }) {
  return <div className="operational-empty"><span aria-hidden="true">∿</span><strong>{title}</strong><p>{detail}</p></div>;
}

function SectionTitle({ label, title, detail }: { label: string; title: string; detail: string }) {
  return <header className="section-title"><span>{label}</span><div><h2>{title}</h2><p>{detail}</p></div></header>;
}

function RolloutView({ data }: { data: RecordValue }) {
  const status = data?.status ?? {};
  const analyses = Array.isArray(data?.analyses) ? data.analyses : [];
  const index = Number(status.currentStepIndex ?? -1);
  const weights = [10, 25, 50, 100];
  return <div className="operational-stack">
    <section className="instrument-panel">
      <SectionTitle label="Traffic" title="Progressive delivery" detail="The active release stays serving until every metric gate agrees." />
      <div className="signal-grid"><Signal label="Strategy" value={data?.strategy}/><Signal label="Phase" value={status.phase}/><Signal label="Ready replicas" value={status.readyReplicas ?? 0}/><Signal label="Current step" value={index >= 0 ? index + 1 : "Stable"}/></div>
      <div className="weight-track" aria-label="Canary traffic progression">{weights.map((weight, step) => <div className={step <= index ? "weight active" : "weight"} key={weight}><span>{weight}%</span><i /></div>)}</div>
    </section>
    <section className="instrument-panel">
      <SectionTitle label="Analysis" title="Metric gates" detail="Success rate, P95 latency, and restart checks for this candidate." />
      {analyses.length ? <div className="event-list">{analyses.map((analysis: RecordValue) => <article key={text(analysis.name)}><State value={analysis.status?.phase}/><div><strong>{text(analysis.name)}</strong><small>{text(analysis.status?.message, "Analysis result reported by Argo Rollouts")}</small></div></article>)}</div> : <EmptyState title="No active analysis" detail="Metric results appear here while a candidate is moving through traffic weights." />}
    </section>
  </div>;
}

function SLOView({ data }: { data: RecordValue }) {
  const results = Array.isArray(data?.data?.result) ? data.data.result : [];
  const value = results[0]?.value?.[1];
  return <section className="instrument-panel">
    <SectionTitle label="Service level" title="Five-minute request signal" detail="A bounded native summary from the platform Prometheus, not an unrestricted query console." />
    <div className="focus-reading"><span>Request rate</span><strong>{value === undefined ? "No samples" : `${Number(value).toFixed(2)} req/s`}</strong><State value={data?.status === "success" ? "Healthy" : data?.status}/></div>
    <div className="sparkline" aria-hidden="true"><svg viewBox="0 0 600 90" preserveAspectRatio="none"><path d="M0 64 C45 63 62 54 104 56 S172 70 214 48 S286 34 326 42 S396 60 438 36 S520 22 600 28"/><line x1="0" y1="72" x2="600" y2="72"/></svg></div>
  </section>;
}

function LogsView({ data }: { data: RecordValue }) {
  const streams = Array.isArray(data?.data?.result) ? data.data.result : [];
  const entries = streams.flatMap((stream: RecordValue) => (Array.isArray(stream.values) ? stream.values : []).map((value: unknown[]) => ({ timestamp: value[0], line: value[1], labels: stream.stream }))).slice(-100).reverse();
  return <section className="instrument-panel log-panel">
    <SectionTitle label="Loki" title="Recent application logs" detail="Structured workload output from the last hour, capped at 100 visible entries." />
    {entries.length ? <div className="log-lines">{entries.map((entry: RecordValue, index: number) => { let parsed: RecordValue = {}; try { parsed = JSON.parse(text(entry.line)); } catch { parsed = {}; } return <article key={`${entry.timestamp}-${index}`}><time>{entry.timestamp ? new Date(Number(entry.timestamp) / 1e6).toLocaleTimeString() : "—"}</time><span className={`log-level level-${text(parsed.level, "info").toLowerCase()}`}>{text(parsed.level, "info")}</span><code>{text(parsed.message ?? entry.line)}</code><small>{text(parsed.request_id ?? parsed.requestID, "")}</small></article>; })}</div> : <EmptyState title="No logs in this window" detail="Generate application traffic or confirm that logs are enabled for the workload." />}
  </section>;
}

function TracesView({ data }: { data: RecordValue }) {
  const traces = Array.isArray(data?.traces) ? data.traces : [];
  return <section className="instrument-panel">
    <SectionTitle label="Tempo" title="Distributed traces" detail="Recent request paths with duration and root service context." />
    {traces.length ? <div className="trace-list">{traces.map((trace: RecordValue) => <article key={text(trace.traceID)}><span className="trace-mark"/><div><strong>{text(trace.rootTraceName ?? trace.traceID)}</strong><small>{text(trace.rootServiceName, "Application request")}</small></div><code>{text(trace.durationMs ?? trace.duration, "—")} ms</code></article>)}</div> : <EmptyState title="No traces found" detail="Send a request through the Gateway, then return when Tempo has indexed it." />}
  </section>;
}

function PolicyView({ data }: { data: RecordValue[] }) {
  return <section className="instrument-panel">
    <SectionTitle label="Admission" title="Policy and provenance" detail="Kyverno decisions for this application and its signed workload." />
    {data.length ? <div className="event-list">{data.map((report, index) => <article key={text(report.name, String(index))}><State value={report.status?.summary?.fail ? "Failed" : "Passed"}/><div><strong>{text(report.name)}</strong><small>{text(report.status?.summary ? `${report.status.summary.pass ?? 0} passed · ${report.status.summary.fail ?? 0} failed` : "Policy report available")}</small></div></article>)}</div> : <EmptyState title="No policy findings" detail="No PolicyReport findings are associated with this application." />}
  </section>;
}

function DoctorView({ data }: { data: RecordValue[] }) {
  return <section className="instrument-panel">
    <SectionTitle label="Diagnosis" title="Dependency path" detail="Checks run in delivery order so the first failing boundary is actionable." />
    <div className="diagnostic-timeline">{data.map((check, index) => <article key={`${text(check.name)}-${index}`}><span className="diagnostic-index">{String(index + 1).padStart(2, "0")}</span><State value={check.status}/><div><strong>{text(check.name)}</strong><p>{text(check.details)}</p>{check.remediation && <code>{text(check.remediation)}</code>}</div></article>)}</div>
  </section>;
}

function KeyValueView({ data }: { data: RecordValue }) {
  const entries = Object.entries(data ?? {}).filter(([, value]) => typeof value !== "object").slice(0, 16);
  return <section className="instrument-panel"><SectionTitle label="Runtime" title="Operational summary" detail="Curated values returned by the trusted local platform service."/><dl className="signal-grid">{entries.map(([key, value]) => <Signal key={key} label={key.replace(/([A-Z])/g, " $1")} value={value}/>)}</dl></section>;
}

export function ApplicationOperationalView({ view, data }: { view: string; data: any }) {
  switch (view) {
    case "rollout": return <RolloutView data={data ?? {}}/>;
    case "slo": return <SLOView data={data ?? {}}/>;
    case "logs": return <LogsView data={data ?? {}}/>;
    case "traces": return <TracesView data={data ?? {}}/>;
    case "policy": return <PolicyView data={Array.isArray(data) ? data : []}/>;
    case "doctor": return <DoctorView data={Array.isArray(data) ? data : []}/>;
    default: return <KeyValueView data={data ?? {}}/>;
  }
}

export function DatabaseOperationalView({ view, data }: { view: string; data: any }) {
  if (view === "backups") {
    const backups = Array.isArray(data) ? data : [];
    return <section className="instrument-panel"><SectionTitle label="Archive" title="Backup history" detail="External backup records remain outside the disposable cluster."/>{backups.length ? <div className="event-list">{backups.map((backup: RecordValue) => <article key={text(backup.name)}><State value={backup.status?.phase}/><div><strong>{text(backup.name)}</strong><small>{text(backup.createdAt, "Backup timestamp unavailable")}</small></div></article>)}</div> : <EmptyState title="No backup records" detail="Backups appear after CloudNativePG completes the first archive."/>}</section>;
  }
  const summary = data?.summary ?? {};
  const status = data?.status ?? {};
  const spec = data?.spec ?? {};
  return <div className="operational-stack"><section className="instrument-panel"><SectionTitle label="PostgreSQL" title="Database lifetime" detail="Readiness, storage, and connection metadata without secret values."/><div className="signal-grid"><Signal label="Phase" value={summary.phase ?? status.phase}/><Signal label="Ready" value={summary.ready}/><Signal label="Instances" value={spec.instances}/><Signal label="Storage" value={spec.storage?.size}/><Signal label="Connection secret" value={status.connectionSecretName}/><Signal label="Last backup" value={status.lastSuccessfulBackup}/></div></section><section className="instrument-panel"><SectionTitle label="Recovery" title="Archive identity" detail="Every database lifetime writes to a distinct immutable backup server."/><div className="identity-block"><span>Backup server</span><code>{text(status.backupServerName)}</code><span>Recovery source</span><code>{text(status.recoverySourceServerName, "New database lifetime")}</code></div></section></div>;
}
