import { useQuery } from "@tanstack/react-query";
import { Activity, Box, Database, GitBranch, Sparkles } from "lucide-react";
import { api, titleCase } from "@/lib/api";
import {
  Card,
  ErrorState,
  HealthIcon,
  Loading,
  PageHeader,
} from "@/components/ui";

type System = {
  mode: string;
  workspace_id: string;
  workspace_profile: string;
  version: string;
  database: Record<string, string>;
  knowledge: Record<string, string>;
  integrations: Array<Record<string, string>>;
  counts: Record<string, number>;
  warnings: Array<{ code: string; message: string }>;
};
export function Overview() {
  const query = useQuery({
    queryKey: ["system"],
    queryFn: () => api<System>("/api/v1/system"),
    refetchInterval: 15000,
  });
  if (query.isLoading) return <Loading label="Reading workspace health" />;
  if (query.error) return <ErrorState error={query.error} />;
  const s = query.data!;
  const stats = [
    ["Active runs", s.counts.runs_running || 0, Activity],
    ["Review ready", s.counts.runs_completed || 0, GitBranch],
    ["Sandboxes", s.counts.sandboxes || 0, Box],
    ["Total runs", s.counts.runs || 0, Sparkles],
  ] as const;
  return (
    <>
      <PageHeader
        eyebrow={`${titleCase(s.mode)} workspace`}
        title="Operational overview"
        description="Runs, infrastructure, integrations, and knowledge health in one place."
      />
      <div className="stat-grid">
        {stats.map(([label, value, Icon]) => (
          <Card key={label} className="stat-card">
            <Icon size={19} />
            <strong>{value}</strong>
            <span>{label}</span>
          </Card>
        ))}
      </div>
      <div className="two-column">
        <Card>
          <div className="card-heading">
            <div>
              <p className="eyebrow">System</p>
              <h2>Runtime health</h2>
            </div>
            <span className="version-chip">ves {s.version}</span>
          </div>
          <div className="health-list">
            <HealthIcon state={s.database.status} />
            <div>
              <strong>Database</strong>
              <span>
                {titleCase(s.database.backend)} · {titleCase(s.database.status)}
              </span>
            </div>
            <HealthIcon state={s.knowledge.status} />
            <div>
              <strong>Knowledge</strong>
              <span>
                {titleCase(s.knowledge.retrieval_mode)} ·{" "}
                {titleCase(s.knowledge.status)}
              </span>
            </div>
          </div>
        </Card>
        <Card>
          <div className="card-heading">
            <div>
              <p className="eyebrow">Connections</p>
              <h2>Integrations</h2>
            </div>
          </div>
          <div className="integration-list">
            {s.integrations.map((item, i) => (
              <div key={`${item.provider}-${i}`}>
                <HealthIcon state={item.status} />
                <span>
                  <strong>{titleCase(item.provider)}</strong>
                  <small>{titleCase(item.status)}</small>
                </span>
              </div>
            ))}
          </div>
        </Card>
      </div>
      {s.warnings?.length > 0 && (
        <Card className="warning-card">
          <h2>Needs attention</h2>
          {s.warnings.map((w) => (
            <p key={w.code}>
              <Database size={16} />
              {w.message}
            </p>
          ))}
        </Card>
      )}
    </>
  );
}
