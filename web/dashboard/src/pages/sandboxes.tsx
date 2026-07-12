import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
export function Sandboxes() {
  const client = useQueryClient();
  const q = useQuery({
    queryKey: ["sandboxes"],
    queryFn: () => api<{ items: any[] }>("/api/v1/sandboxes?limit=100"),
    refetchInterval: 7000,
  });
  const action = useMutation({
    mutationFn: ({ id, path, body }: { id: string; path: string; body: any }) =>
      api(`/api/v1/sandboxes/${id}/${path}`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () => void client.invalidateQueries({ queryKey: ["sandboxes"] }),
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  const items = q.data?.items || [];
  return (
    <>
      <PageHeader
        eyebrow="Environments"
        title="Sandboxes"
        description="Inspect preview readiness, leases, branches, and lifecycle state."
      />
      <div className="card-grid">
        {items.map((s) => (
          <Card key={s.id}>
            <div className="card-heading">
              <div>
                <p className="eyebrow">{s.backend}</p>
                <h2>{s.id}</h2>
              </div>
              <Badge status={s.status} />
            </div>
            <dl className="detail-list">
              <dt>Run</dt>
              <dd>{s.run_id || "Unattached"}</dd>
              <dt>Branch</dt>
              <dd>{s.branch || "—"}</dd>
              <dt>Last accessed</dt>
              <dd>{fmtTime(s.last_accessed_at)}</dd>
              <dt>Expires</dt>
              <dd>{fmtTime(s.expires_at)}</dd>
            </dl>
            <div className="row-actions">
              <Button
                variant="secondary"
                onClick={() =>
                  action.mutate({
                    id: s.id,
                    path: "retain",
                    body: { hours: 24, confirmed: true },
                  })
                }
              >
                Retain 24h
              </Button>
              <Button
                variant="danger"
                onClick={() =>
                  confirm("Destroy this sandbox?") &&
                  action.mutate({
                    id: s.id,
                    path: "destroy",
                    body: { confirmed: true },
                  })
                }
              >
                Destroy
              </Button>
            </div>
          </Card>
        ))}
        {items.length === 0 && (
          <Card>
            <Empty
              title="No sandboxes"
              detail="Active and retained environments will appear here."
            />
          </Card>
        )}
      </div>
      {action.error && <ErrorState error={action.error} />}
    </>
  );
}
