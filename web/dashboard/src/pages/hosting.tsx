import { useMutation } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Cloud, ShieldCheck } from "lucide-react";
import { api } from "@/lib/api";
import { Badge, Button, Card, ErrorState, PageHeader } from "@/components/ui";

type Promotion = {
  id: string;
  status: "pending" | "running" | "completed" | "failed";
  stage: string;
  attempts: number;
  error?: string;
  result_json?: string;
};
type PromotionEvent = {
  seq: number;
  stage: string;
  status: string;
  message: string;
  created_at: string;
};

export function Hosting() {
  const [name, setName] = useState("vessica-control-plane");
  const [previewOrigin, setPreviewOrigin] = useState("");
  const [activeID, setActiveID] = useState(
    () => localStorage.getItem("vessica-promotion") || "",
  );
  const [operation, setOperation] = useState<Promotion | null>(null);
  const [events, setEvents] = useState<PromotionEvent[]>([]);
  const promotion = useMutation({
    mutationFn: () =>
      api<Promotion>("/api/v1/hosting/promotions", {
        method: "POST",
        body: JSON.stringify({ name, preview_origin: previewOrigin }),
      }),
    onSuccess: (value) => {
      localStorage.setItem("vessica-promotion", value.id);
      localStorage.removeItem(`vessica-promotion-seq:${value.id}`);
      setEvents([]);
      setOperation(value);
      setActiveID(value.id);
    },
  });
  const resume = useMutation({
    mutationFn: () =>
      api<Promotion>(`/api/v1/hosting/promotions/${activeID}/resume`, {
        method: "POST",
        body: JSON.stringify({ confirmed: true }),
      }),
    onSuccess: (value) => setOperation({ ...value, status: "running" }),
  });

  useEffect(() => {
    if (!activeID) return;
    let closed = false;
    api<Promotion>(`/api/v1/hosting/promotions/${activeID}`)
      .then(setOperation)
      .catch(() => localStorage.removeItem("vessica-promotion"));
    const after =
      localStorage.getItem(`vessica-promotion-seq:${activeID}`) || "0";
    const stream = new EventSource(
      `/api/v1/hosting/promotions/${activeID}/stream?after=${encodeURIComponent(after)}`,
    );
    stream.onmessage = (message) => {
      const value = JSON.parse(message.data) as PromotionEvent;
      localStorage.setItem(
        `vessica-promotion-seq:${activeID}`,
        String(value.seq),
      );
      setEvents((current) =>
        current.some((event) => event.seq === value.seq)
          ? current
          : [...current, value].slice(-100),
      );
    };
    stream.addEventListener("result", (message) => {
      const value = JSON.parse((message as MessageEvent).data) as Promotion;
      setOperation(value);
      stream.close();
      closed = true;
    });
    stream.onerror = () => {
      if (closed) return;
      api<Promotion>(`/api/v1/hosting/promotions/${activeID}`)
        .then((value) => {
          setOperation(value);
          if (value.status === "completed" || value.status === "failed") {
            stream.close();
            closed = true;
          }
        })
        .catch(() => undefined);
    };
    return () => stream.close();
  }, [activeID]);
  let result: { owner_claim_url?: string; recovery_snapshot?: string } = {};
  try {
    result = operation?.result_json ? JSON.parse(operation.result_json) : {};
  } catch {
    result = {};
  }
  return (
    <>
      <PageHeader
        eyebrow="Local to hosted"
        title="Move to Railway"
        description="Provision a hosted control plane without changing local authority until every verification passes."
      />
      <div className="two-column">
        <Card>
          <Cloud className="feature-icon" />
          <h2>What Vessica creates</h2>
          <ul className="feature-list">
            <li>Control plane and isolated preview origin</li>
            <li>Postgres state and hosted knowledge service</li>
            <li>Encrypted provider credentials and worker checkpoint</li>
            <li>Verified state migration with a local recovery snapshot</li>
          </ul>
        </Card>
        <Card>
          <ShieldCheck className="feature-icon" />
          <h2>Start promotion</h2>
          <label>
            Railway project name
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <label>
            Preview origin
            <input
              value={previewOrigin}
              onChange={(e) => setPreviewOrigin(e.target.value)}
              placeholder="https://previews.example.com"
              type="url"
            />
          </label>
          <p className="muted">
            Use an HTTPS hostname dedicated to untrusted preview applications.
            Railway will return any required DNS records.
          </p>
          <div className="notice">
            <strong>Safe by default</strong>
            <span>
              A failure leaves this local workspace authoritative and resumable.
            </span>
          </div>
          <Button
            disabled={
              promotion.isPending ||
              !name.trim() ||
              !previewOrigin.startsWith("https://")
            }
            onClick={() =>
              confirm(
                "Provision Railway infrastructure and promote this workspace?",
              ) && promotion.mutate()
            }
          >
            {promotion.isPending ? "Starting…" : "Run checks and continue"}
          </Button>
          {activeID && (
            <p className="success-copy">
              Promotion {activeID} is retained. You can refresh without starting
              it twice.
            </p>
          )}
          {promotion.error && <ErrorState error={promotion.error} />}
          {resume.error && <ErrorState error={resume.error} />}
        </Card>
      </div>
      {operation && (
        <Card>
          <div className="section-heading">
            <div>
              <p className="eyebrow">Durable operation</p>
              <h2>{operation.stage}</h2>
            </div>
            <Badge status={operation.status} />
          </div>
          {operation.error && <ErrorState error={new Error(operation.error)} />}
          <ol className="event-list" aria-label="Promotion progress">
            {events.map((event) => (
              <li key={event.seq}>
                <Badge status={event.status} />
                <span>{event.message}</span>
              </li>
            ))}
          </ol>
          {operation.status === "failed" && (
            <Button
              variant="secondary"
              disabled={resume.isPending}
              onClick={() =>
                confirm("Retry this promotion from its retained operation?") &&
                resume.mutate()
              }
            >
              {resume.isPending ? "Resuming…" : "Resume promotion"}
            </Button>
          )}
          {operation.status === "completed" &&
            result.owner_claim_url?.startsWith("https://") && (
              <Button
                onClick={() =>
                  window.open(result.owner_claim_url, "_blank", "noopener")
                }
              >
                Claim and open hosted dashboard
              </Button>
            )}
          {result.recovery_snapshot && (
            <p className="muted">
              Local recovery snapshot: <code>{result.recovery_snapshot}</code>
            </p>
          )}
        </Card>
      )}
    </>
  );
}
