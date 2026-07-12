import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, fmtTime } from "@/lib/api";
import {
  Badge,
  Card,
  Empty,
  ErrorState,
  Loading,
  PageHeader,
} from "@/components/ui";
type Run = {
  id: string;
  epic_id?: string;
  status: string;
  current_phase?: string;
  runner?: string;
  model?: string;
  sandbox_backend?: string;
  updated_at: string;
  pr_url?: string;
};
export function Runs() {
  const q = useQuery({
    queryKey: ["runs"],
    queryFn: () => api<{ items: Run[] }>("/api/v1/runs?limit=100"),
    refetchInterval: 5000,
  });
  if (q.isLoading) return <Loading label="Loading runs" />;
  if (q.error) return <ErrorState error={q.error} />;
  const runs = q.data?.items || [];
  return (
    <>
      <PageHeader
        eyebrow="Execution"
        title="Runs"
        description="Monitor every workflow from queue to receipt."
      />
      {runs.length === 0 ? (
        <Card>
          <Empty
            title="No runs yet"
            detail="Runs started from the CLI, Codex, Linear, or Jira will appear here."
          />
        </Card>
      ) : (
        <Card className="table-card">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Run</th>
                  <th>Status</th>
                  <th>Phase</th>
                  <th>Runner</th>
                  <th>Sandbox</th>
                  <th>Updated</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => (
                  <tr key={run.id}>
                    <td>
                      <Link className="entity-link" to={`/runs/${run.id}`}>
                        {run.id}
                      </Link>
                      <small>{run.epic_id || "No epic"}</small>
                    </td>
                    <td>
                      <Badge status={run.status} />
                    </td>
                    <td>{run.current_phase || "—"}</td>
                    <td>
                      {run.runner || "—"}
                      <small>{run.model}</small>
                    </td>
                    <td>{run.sandbox_backend || "—"}</td>
                    <td>{fmtTime(run.updated_at)}</td>
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
