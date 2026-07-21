import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api, fmtTime } from "@/lib/api";
import {
  Badge,
  Button,
  Card,
  Empty,
  ErrorState,
  Loading,
  PageHeader,
} from "@/components/ui";

type Agent = {
  id: string;
  name: string;
  purpose: string;
  state: string;
  model: string;
  reasoning_effort: string;
  heartbeat_enabled: boolean;
  next_run_at?: string;
  budget_limit_microusd: number;
  budget_spent_microusd: number;
  evaluation_score: number;
  last_run?: AgentRunRecord;
};
type AgentRunRecord = {
  id: string;
  agent_id: string;
  trigger: string;
  status: string;
  created_at: string;
  updated_at: string;
  output_json?: string;
  terminal_error?: string;
  reservation_microusd: number;
};
type Event = {
  id: string;
  seq: number;
  type: string;
  payload_json: string;
  attempt_id: string;
  created_at: string;
};
type Attempt = {
  id: string;
  attempt_number: number;
  status: string;
  started_at: string;
  finished_at?: string;
  usage_json: string;
  error?: string;
};
const usd = (micro?: number) => `$${((micro || 0) / 1_000_000).toFixed(2)}`;
const changedDefinitionFields = (newer: string, older?: string) => {
  if (!older) return ["initial definition"];
  try {
    const current = JSON.parse(newer);
    const previous = JSON.parse(older);
    return Array.from(new Set([...Object.keys(current), ...Object.keys(previous)]))
      .filter((key) => JSON.stringify(current[key]) !== JSON.stringify(previous[key]));
  } catch {
    return ["definition"];
  }
};

export function Agents() {
  const client = useQueryClient();
  const [description, setDescription] = useState("");
  const [review, setReview] = useState(false);
  const [advanced, setAdvanced] = useState(false);
  const [definition, setDefinition] = useState(`{
  "kind": "vessica.agent/v1",
  "name": "AGENT",
  "purpose": "",
  "system_prompt": "",
  "budget": {"daily_usd": "5.00", "timezone": "${Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"}"}
}`);
  const q = useQuery({
    queryKey: ["agents"],
    queryFn: () => api<{ agents: Agent[] }>("/api/v1/agents"),
    refetchInterval: 5000,
  });
  const system = useQuery({
    queryKey: ["system"],
    queryFn: () => api<any>("/api/v1/system"),
    refetchInterval: 15000,
  });
  const create = useMutation({
    mutationFn: () =>
      advanced
        ? api("/api/v1/agents", {
            method: "POST",
            body: JSON.stringify(JSON.parse(definition)),
          })
        : api("/api/v1/agent-builds", {
            method: "POST",
            body: JSON.stringify({ description, review, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC" }),
          }),
    onSuccess: () => {
      setDescription("");
      void client.invalidateQueries({ queryKey: ["agents"] });
    },
  });
  if (q.isLoading) return <Loading label="Loading agents" />;
  if (q.error) return <ErrorState error={q.error} />;
  const agents = q.data?.agents || [];
  return (
    <>
      <PageHeader
        eyebrow="Automation"
        title="Agents"
        description="Durable cloud definitions instantiated for manual, scheduled, child, and evaluation runs."
      />
      {system.data?.agent_runtime && !system.data.agent_runtime.accepted && (
        <Card className="credential-banner">
          <h2>OpenAI credentials required</h2>
          <p>
            The runtime service is installed but agent builds and runs remain
            inactive until credentials and capabilities are ready.
          </p>
          <code>ves auth login openai --env OPENAI_API_KEY</code>
        </Card>
      )}
      <Card>
        <h2>Build an agent</h2>
        <label className="inline-control">
          <input
            type="checkbox"
            checked={advanced}
            onChange={(e) => setAdvanced(e.target.checked)}
          />{" "}
          Use structured definition
        </label>
        {advanced ? (
          <label>
            vessica.agent/v1 JSON
            <textarea
              rows={14}
              value={definition}
              onChange={(e) => setDefinition(e.target.value)}
            />
          </label>
        ) : (
          <>
            <label>
              Describe what the agent should do
              <textarea
                rows={4}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Research competitor announcements every weekday and summarize material changes."
              />
            </label>
            <label className="inline-control">
              <input
                type="checkbox"
                checked={review}
                onChange={(e) => setReview(e.target.checked)}
              />{" "}
              Review before activation
            </label>
          </>
        )}
        <Button
          disabled={(!advanced && !description.trim()) || create.isPending}
          onClick={() => create.mutate()}
        >
          Build agent
        </Button>
        {create.error && <ErrorState error={create.error} />}
      </Card>
      {agents.length === 0 ? (
        <Card>
          <Empty
            title="No agents yet"
            detail="Describe an agent above or create one with ves agent create."
          />
        </Card>
      ) : (
        <Card className="table-card">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Agent</th>
                  <th>Status</th>
                  <th>Model</th>
                  <th>Heartbeat</th>
                  <th>Budget</th>
                  <th>Last run</th>
                  <th>Eval</th>
                </tr>
              </thead>
              <tbody>
                {agents.map((a) => (
                  <tr key={a.id}>
                    <td>
                      <Link className="entity-link" to={`/agents/${a.id}`}>
                        {a.name}
                      </Link>
                      <small>{a.purpose}</small>
                    </td>
                    <td>
                      <Badge status={a.state} />
                    </td>
                    <td>
                      {a.model}
                      <small>{a.reasoning_effort}</small>
                    </td>
                    <td>
                      {a.heartbeat_enabled
                        ? fmtTime(a.next_run_at)
                        : "Disabled"}
                    </td>
                    <td>
                      {usd(a.budget_spent_microusd)} /{" "}
                      {usd(a.budget_limit_microusd)}
                    </td>
                    <td>
                      {a.last_run ? (
                        <Link to={`/agent-runs/${a.last_run.id}`}>
                          <Badge status={a.last_run.status} />
                        </Link>
                      ) : (
                        "—"
                      )}
                    </td>
                    <td>
                      {a.evaluation_score
                        ? `${(a.evaluation_score * 100).toFixed(0)}%`
                        : "—"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </>
  );
}

export function AgentDetail() {
  const { id = "" } = useParams();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [prompt, setPrompt] = useState("");
  const [daily, setDaily] = useState("");
  const [cron, setCron] = useState("");
  const [editDefinition, setEditDefinition] = useState("");
  const [editError, setEditError] = useState<Error>();
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  const q = useQuery({
    queryKey: ["agent", id],
    queryFn: () => api<any>(`/api/v1/agents/${id}`),
    refetchInterval: 4000,
  });
  const mutate = useMutation({
    mutationFn: ({
      path,
      method = "POST",
      body,
    }: {
      path: string;
      method?: string;
      body?: unknown;
    }) => api(path, { method, body: body ? JSON.stringify(body) : undefined }),
    onSuccess: () => void client.invalidateQueries({ queryKey: ["agent", id] }),
  });
  const run = useMutation({
    mutationFn: () =>
      api<AgentRunRecord>(`/api/v1/agents/${id}/runs`, {
        method: "POST",
        body: JSON.stringify({
          prompt,
          repository_id: localStorage.getItem("vessica-repository-id") || "",
        }),
      }),
    onSuccess: (r) => navigate(`/agent-runs/${r.id}`),
  });
  if (q.isLoading) return <Loading label="Loading agent" />;
  if (q.error) return <ErrorState error={q.error} />;
  const d = q.data;
  return (
    <>
      <PageHeader
        eyebrow="Agent"
        title={d.agent.name}
        description={d.agent.purpose}
        actions={
          <>
            <Badge status={d.agent.state} />
            {d.agent.state === "active" ? (
              <Button
                variant="secondary"
                onClick={() =>
                  mutate.mutate({ path: `/api/v1/agents/${id}/pause` })
                }
              >
                Pause
              </Button>
            ) : d.agent.state === "paused" ? (
              <Button
                onClick={() =>
                  mutate.mutate({ path: `/api/v1/agents/${id}/resume` })
                }
              >
                Resume
              </Button>
            ) : null}
            {d.agent.state !== "archived" && (
              <Button
                variant="secondary"
                onClick={() =>
                  mutate.mutate({ path: `/api/v1/agents/${id}/archive` })
                }
              >
                Archive
              </Button>
            )}
          </>
        }
      />
      <nav className="agent-tabs" aria-label="Agent detail sections">
        <a href="#definition">Definition</a>
        <a href="#tools-knowledge">Tools &amp; knowledge</a>
        <a href="#schedule-budget">Schedule &amp; budget</a>
        <a href="#runs">Runs</a>
        <a href="#evaluations">Evaluations</a>
        <a href="#versions">Versions</a>
      </nav>
      <div className="detail-grid">
        <Card>
          <h2>Run now</h2>
          <textarea
            rows={4}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="What should this run accomplish?"
          />
          <Button
            disabled={!prompt.trim() || run.isPending}
            onClick={() => run.mutate()}
          >
            Start run
          </Button>
          {run.error && <ErrorState error={run.error} />}
        </Card>
        <Card id="schedule-budget">
          <h2>Schedule & budget</h2>
          <p>
            <strong>Spend:</strong> {usd(d.budget.spent_microusd)} of{" "}
            {usd(d.budget.daily_limit_microusd)}
          </p>
          <div className="inline-form">
            <input
              value={daily}
              onChange={(e) => setDaily(e.target.value)}
              placeholder="Daily USD"
            />
            <Button
              variant="secondary"
              onClick={() =>
                mutate.mutate({
                  path: `/api/v1/agents/${id}/budget`,
                  method: "PUT",
                  body: { daily_usd: daily, timezone },
                })
              }
            >
              Update
            </Button>
          </div>
          <div className="inline-form">
            <input
              value={cron}
              onChange={(e) => setCron(e.target.value)}
              placeholder="0 9 * * 1"
            />
            <Button
              variant="secondary"
              onClick={() =>
                mutate.mutate({
                  path: `/api/v1/agents/${id}/heartbeat`,
                  method: "PUT",
                  body: { enabled: true, cron, timezone },
                })
              }
            >
              Set heartbeat
            </Button>
          </div>
          <p className="muted">
            {d.schedule?.enabled
              ? `Next ${fmtTime(d.schedule.next_due_at)} (${d.schedule.timezone})`
              : "Heartbeat disabled"}
          </p>
          {d.schedule?.enabled && (
            <Button
              variant="secondary"
              onClick={() =>
                mutate.mutate({
                  path: `/api/v1/agents/${id}/heartbeat`,
                  method: "DELETE",
                })
              }
            >
              Disable heartbeat
            </Button>
          )}
        </Card>
      </div>
      <Card id="definition">
        <h2>Definition v{d.version.version}</h2>
        <dl className="facts">
          <div>
            <dt>Model</dt>
            <dd>
              {d.definition.model.id} · {d.definition.model.reasoning_effort}
            </dd>
          </div>
          <div>
            <dt>System prompt</dt>
            <dd>
              <pre>{d.definition.system_prompt}</pre>
            </dd>
          </div>
        </dl>
        <details>
          <summary>Edit structured definition</summary>
          <textarea
            rows={18}
            value={editDefinition || JSON.stringify(d.definition, null, 2)}
            onChange={(e) => setEditDefinition(e.target.value)}
          />
          <Button
            onClick={() => {
              try {
                setEditError(undefined);
                mutate.mutate({
                  path: `/api/v1/agents/${id}`,
                  method: "PATCH",
                  body: JSON.parse(
                    editDefinition || JSON.stringify(d.definition, null, 2),
                  ),
                });
              } catch (error) {
                setEditError(
                  error instanceof Error
                    ? error
                    : new Error("Invalid definition JSON"),
                );
              }
            }}
          >
            Save new version
          </Button>
        </details>
        {mutate.error && <ErrorState error={mutate.error} />}
        {editError && <ErrorState error={editError} />}
      </Card>
      <Card id="tools-knowledge">
        <h2>Tools &amp; knowledge</h2>
        <h3>Enabled tools</h3>
        <p>{d.definition.tools.map((t: any) => t.id).join(", ") || "None"}</p>
        <h3>Knowledge references</h3>
        {d.definition.knowledge.length === 0 ? (
          <p className="muted">No knowledge references.</p>
        ) : (
          d.definition.knowledge.map((k: any) => (
            <p key={`${k.artifact_id}-${k.version}`}>
              <strong>{k.artifact_id}@{k.version}</strong>
              <small>{k.description}</small>
            </p>
          ))
        )}
      </Card>
      <div className="detail-grid">
        <Card id="versions">
          <h2>Version history</h2>
          {d.versions.map((v: any, index: number) => (
            <details key={v.version}>
              <summary>
                Version {v.version} · {fmtTime(v.created_at)}
              </summary>
              <p className="muted">
                Changed: {changedDefinitionFields(v.definition_json, d.versions[index + 1]?.definition_json).join(", ")}
              </p>
              <pre>{v.definition_json}</pre>
            </details>
          ))}
        </Card>
        <Card id="evaluations">
          <h2>Evaluations</h2>
          {d.evaluations.length === 0 ? (
            <p className="muted">No critic evaluations yet.</p>
          ) : (
            d.evaluations.map((e: any) => (
              <p key={e.id}>
                <Link to={`/agent-runs/${e.evaluated_run_id}`}>
                  {e.evaluated_run_id}
                </Link>{" "}
                · <Badge status={e.status} />{" "}
                {e.status === "completed" &&
                  `${(e.score * 100).toFixed(0)}% ${e.passed ? "pass" : "fail"}`}
                <small>{e.summary}</small>
              </p>
            ))
          )}
        </Card>
      </div>
      <Card className="table-card" id="runs">
        <h2>Runs</h2>
        {d.runs.length === 0 ? (
          <Empty title="No runs" detail="Start an ad-hoc run above." />
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Run</th>
                  <th>Trigger</th>
                  <th>Status</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {d.runs.map((r: AgentRunRecord) => (
                  <tr key={r.id}>
                    <td>
                      <Link className="entity-link" to={`/agent-runs/${r.id}`}>
                        {r.id}
                      </Link>
                    </td>
                    <td>{r.trigger}</td>
                    <td>
                      <Badge status={r.status} />
                    </td>
                    <td>{fmtTime(r.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </>
  );
}

export function AgentRun() {
  const { id = "" } = useParams();
  const q = useQuery({
    queryKey: ["agent-run", id],
    queryFn: () =>
      api<{
        run: AgentRunRecord;
        events: Event[];
        attempts: Attempt[];
        evaluation?: any;
      }>(`/api/v1/agent-runs/${id}`),
    refetchInterval: 1000,
  });
  if (q.isLoading) return <Loading label="Opening agent run" />;
  if (q.error) return <ErrorState error={q.error} />;
  const { run, events, attempts, evaluation } = q.data!;
  let attempt = "";
  return (
    <>
      <PageHeader
        eyebrow="Agent run"
        title={run.id}
        description={`${run.trigger} · ${fmtTime(run.created_at)}`}
        actions={<Badge status={run.status} />}
      />
      {run.terminal_error && (
        <ErrorState error={new Error(run.terminal_error)} />
      )}
      <div className="run-layout">
        <Card>
          <h2>Conversation</h2>
          <div className="conversation">
            {events.map((e) => {
              const payload = JSON.parse(e.payload_json || "{}");
              const boundary = e.attempt_id !== attempt;
              attempt = e.attempt_id;
              const child = payload.run_id && e.type.startsWith("agent.child");
              return (
                <div key={e.id}>
                  {boundary && (
                    <div className="attempt-boundary">
                      Attempt{" "}
                      {attempts.find((a) => a.id === e.attempt_id)
                        ?.attempt_number || e.attempt_id}
                    </div>
                  )}
                  <article
                    className={`event-card event-${e.type.replaceAll(".", "-")}`}
                  >
                    <small>
                      {e.type} · #{e.seq}
                    </small>
                    <p>
                      {child ? (
                        <Link to={`/agent-runs/${payload.run_id}`}>
                          {payload.run_id}
                        </Link>
                      ) : (
                        payload.text ||
                        payload.tool ||
                        payload.error ||
                        JSON.stringify(payload)
                      )}
                    </p>
                  </article>
                </div>
              );
            })}
            {events.length === 0 && <Loading label="Waiting for runtime" />}
          </div>
        </Card>
        <Card>
          <h2>Usage & result</h2>
          <p>Reservation: {usd(run.reservation_microusd)}</p>
          {attempts.map((a) => (
            <details key={a.id}>
              <summary>
                Attempt {a.attempt_number} · {a.status}
              </summary>
              <pre>{a.usage_json}</pre>
              {a.error && <p>{a.error}</p>}
            </details>
          ))}
          {run.output_json && <pre>{run.output_json}</pre>}
          {evaluation && (
            <div>
              <h3>Critic result</h3>
              <p>
                {(evaluation.score * 100).toFixed(0)}% ·{" "}
                {evaluation.passed ? "Pass" : "Fail"}
              </p>
              <p>{evaluation.summary}</p>
            </div>
          )}
        </Card>
      </div>
    </>
  );
}
