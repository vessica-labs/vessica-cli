import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  ExternalLink,
  GitCommit,
  Play,
  RotateCcw,
  Send,
  Square,
} from "lucide-react";
import { api, fmtTime, titleCase } from "@/lib/api";
import { presentEvent } from "@/lib/event-presentation";
import {
  Badge,
  Button,
  Card,
  ErrorState,
  Loading,
  PageHeader,
} from "@/components/ui";
type Detail = {
  run: any;
  epic?: any;
  tickets: any[];
  phases: any[];
  sandboxes: any[];
  artifacts: any[];
  evidence: any[];
  receipt?: any;
};
export function RunDetail() {
  const { id = "" } = useParams();
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["run", id],
    queryFn: () => api<Detail>(`/api/v1/runs/${id}`),
    refetchInterval: 5000,
  });
  const [events, setEvents] = useState<any[]>([]);
  const [prompt, setPrompt] = useState("");
  useEffect(() => {
    const key = `vessica-seq:${id}`,
      after = sessionStorage.getItem(key) || "0";
    const source = new EventSource(`/api/v1/runs/${id}/stream?after=${after}`);
    source.addEventListener("event", (e) => {
      const record = JSON.parse((e as MessageEvent).data);
      const seq = record.seq || record.event?.seq;
      if (seq) {
        sessionStorage.setItem(key, String(seq));
        setEvents((prev) =>
          prev.some((v) => v.seq === seq)
            ? prev
            : [...prev, record].slice(-300),
        );
      }
    });
    source.addEventListener("result", () => {
      source.close();
      void client.invalidateQueries({ queryKey: ["run", id] });
    });
    return () => source.close();
  }, [id, client]);
  const action = useMutation({
    mutationFn: ({ path, body }: { path: string; body: any }) =>
      api(`/api/v1/runs/${id}/${path}`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      setPrompt("");
      void client.invalidateQueries({ queryKey: ["run", id] });
    },
  });
  const preview = useMutation({
    mutationFn: () =>
      api<{ url: string }>(`/api/v1/runs/${id}/preview-access`, {
        method: "POST",
        body: "{}",
      }),
    onSuccess: ({ url }) => window.open(url, "_blank", "noopener,noreferrer"),
  });
  if (q.isLoading) return <Loading label="Composing run workspace" />;
  if (q.error) return <ErrorState error={q.error} />;
  const d = q.data!,
    run = d.run;
  return (
    <>
      <PageHeader
        eyebrow={run.epic_id || "Run workspace"}
        title={d.epic?.title || run.id}
        description={`${titleCase(run.workflow)} · updated ${fmtTime(run.updated_at)}`}
        actions={<Badge status={run.status} />}
      />
      <div className="run-grid">
        <div className="run-main">
          <Card>
            <div className="card-heading">
              <div>
                <p className="eyebrow">Workflow</p>
                <h2>Phase progress</h2>
              </div>
            </div>
            <div className="phase-rail">
              {d.phases.map((p: any) => (
                <div className={`phase phase-${p.status}`} key={p.phase}>
                  <span></span>
                  <div>
                    <strong>{titleCase(p.phase)}</strong>
                    <small>{titleCase(p.status)}</small>
                  </div>
                </div>
              ))}
            </div>
          </Card>
          <Card>
            <div className="card-heading">
              <div>
                <p className="eyebrow">Live</p>
                <h2>Agent activity</h2>
              </div>
              <span className="live-dot">Streaming</span>
            </div>
            <div className="timeline" aria-live="polite">
              {events.length === 0 ? (
                <p className="muted">
                  Waiting for new activity. Existing evidence remains below.
                </p>
              ) : (
                events.map((record: any) => {
                  const event = presentEvent(record.event);
                  return (
                    <div key={record.seq} className={`timeline-event ${event.agentMessage ? "timeline-agent-message" : ""}`}>
                      <span>{event.agentMessage ? "A" : "V"}</span>
                      <div>
                        <strong>{event.title}</strong>
                        {event.agentMessage ? (
                          <div className="agent-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{event.message}</ReactMarkdown></div>
                        ) : (
                          <>
                            <p className="event-summary">{event.summary}</p>
                            <details className="event-details">
                              <summary>Show event details</summary>
                              <pre>{event.detail}</pre>
                            </details>
                          </>
                        )}
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </Card>
          <Card>
            <div className="card-heading">
              <div>
                <p className="eyebrow">Refine</p>
                <h2>Request a focused change</h2>
              </div>
            </div>
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              maxLength={4000}
              placeholder="Describe the revision to apply in the retained sandbox…"
            />
            <div className="composer-actions">
              <small>{prompt.length}/4000</small>
              <Button
                disabled={!prompt.trim() || action.isPending}
                onClick={() =>
                  action.mutate({
                    path: "refinements",
                    body: { prompt, confirmed: true },
                  })
                }
              >
                <Send size={15} /> Send refinement
              </Button>
            </div>
            {action.error && <ErrorState error={action.error} />}
          </Card>
          <Card>
            <div className="card-heading">
              <div>
                <p className="eyebrow">Evidence</p>
                <h2>Validation and artifacts</h2>
              </div>
            </div>
            <div className="evidence-list">
              {d.evidence.map((e: any) => (
                <div key={e.id}>
                  <Badge status={e.status} />
                  <span>
                    <strong>{titleCase(e.kind)}</strong>
                    <small>
                      {titleCase(e.phase)} · {fmtTime(e.created_at)}
                    </small>
                  </span>
                </div>
              ))}
              {d.artifacts.map((a: any) => (
                <div key={a.id}>
                  <GitCommit size={17} />
                  <span>
                    <strong>{a.title}</strong>
                    <small>{a.type}</small>
                  </span>
                </div>
              ))}
            </div>
          </Card>
        </div>
        <aside className="run-side">
          <Card>
            <p className="eyebrow">Review</p>
            <h2>Decision</h2>
            <div className="stack-actions">
              <Button
                onClick={() =>
                  action.mutate({ path: "approve", body: { confirmed: true } })
                }
              >
                <Play size={15} /> Approve and merge
              </Button>
              <Button
                variant="secondary"
                onClick={() =>
                  action.mutate({ path: "rollback", body: { confirmed: true } })
                }
              >
                <RotateCcw size={15} /> Roll back
              </Button>
              <Button
                variant="danger"
                onClick={() =>
                  action.mutate({ path: "cancel", body: { confirmed: true } })
                }
              >
                <Square size={14} /> Cancel run
              </Button>
            </div>
          </Card>
          <Card>
            <p className="eyebrow">References</p>
            <h2>Delivery</h2>
            <dl className="detail-list">
              <dt>Runner</dt>
              <dd>{run.runner || "—"}</dd>
              <dt>Model</dt>
              <dd>{run.model || "—"}</dd>
              <dt>Branch / PR</dt>
              <dd>
                {run.pr_url ? (
                  <a href={run.pr_url} target="_blank">
                    Open pull request <ExternalLink size={13} />
                  </a>
                ) : (
                  "—"
                )}
              </dd>
              <dt>Preview</dt>
              <dd>
                {run.preview_url ? (
                  <button
                    className="link-button"
                    type="button"
                    onClick={() => preview.mutate()}
                    disabled={preview.isPending}
                  >
                    Open preview <ExternalLink size={13} />
                  </button>
                ) : (
                  "Unavailable"
                )}
              </dd>
              <dt>Receipt</dt>
              <dd>{run.receipt_id || "Pending"}</dd>
            </dl>
          </Card>
          {d.sandboxes.map((s: any) => (
            <Card key={s.id}>
              <p className="eyebrow">Sandbox</p>
              <h2>{s.id}</h2>
              <Badge status={s.status} />
              <dl className="detail-list">
                <dt>Backend</dt>
                <dd>{s.backend}</dd>
                <dt>Branch</dt>
                <dd>{s.branch || "—"}</dd>
                <dt>Expires</dt>
                <dd>{fmtTime(s.expires_at)}</dd>
              </dl>
            </Card>
          ))}
        </aside>
      </div>
    </>
  );
}
