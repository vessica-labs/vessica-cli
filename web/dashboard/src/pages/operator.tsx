import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, fmtTime } from "@/lib/api";
import { Badge, Card, Empty, ErrorState, Loading, PageHeader } from "@/components/ui";

type Snapshot = {
  oauth_failures: number; mcp_calls: number; mcp_errors: number; mcp_latency_ms: number; denied_actions: number; rejected_records: number; stale_ingestion_batches: number;
  stale_checkpoints: Array<{ source_type: string; source_id: string; updated_at: string }>;
  failed_agents: Array<{ id: string; agent_id: string; terminal_error?: string; updated_at: string }>;
  missing_briefings: string[];
  budgets: Array<{ agent_id: string; agent_name: string; limit_microusd: number; reserved_microusd: number; spent_microusd: number }>;
  recent_actions: Array<{ ID: string; Tool: string; PolicyDecision: string; ExecutionState: string; LatencyMS: number; AgentRunID?: string; CreatedAt: string }>;
};

export function Operator() {
  const query = useQuery({ queryKey: ["operator"], queryFn: () => api<Snapshot>("/api/v1/operator"), refetchInterval: 15_000 });
  const data = query.data;
  return <>
    <PageHeader eyebrow="Owner only" title="Cloud agent operations" description="OAuth, MCP, ingestion, source, briefing, agent, budget, and policy signals in one workspace-scoped view." actions={<a className="button button-secondary" href="/internal/dashboard/metrics">Prometheus metrics</a>} />
    {query.isLoading && <Loading label="Loading operator signals" />}
    {query.error && <ErrorState error={query.error} />}
    {data && <>
      <div className="stat-grid"><Card><strong>{data.oauth_failures}</strong><span>OAuth failures</span></Card><Card><strong>{data.mcp_errors}</strong><span>MCP errors / {data.mcp_calls} calls</span></Card><Card><strong>{data.rejected_records}</strong><span>Rejected records</span></Card><Card><strong>{data.denied_actions}</strong><span>Denied actions</span></Card></div>
      <div className="two-column">
        <Card><h2>Sources & briefings</h2>{!data.stale_checkpoints.length && !data.missing_briefings.length && !data.stale_ingestion_batches && <Empty title="Fresh" detail="No stale checkpoints, ingestion batches, or missing artifacts." />}{data.stale_ingestion_batches > 0 && <div className="operator-row"><span><strong>{data.stale_ingestion_batches} stale ingestion batches</strong><small>Incomplete for more than 24 hours</small></span><Badge status="stale" /></div>}{data.stale_checkpoints.map((item) => <div className="operator-row" key={`${item.source_type}:${item.source_id}`}><span><strong>{item.source_id}</strong><small>{item.source_type} · {fmtTime(item.updated_at)}</small></span><Badge status="stale" /></div>)}{data.missing_briefings.map((key) => <div className="operator-row" key={key}><span><strong>{key}</strong><small>Expected canonical artifact</small></span><Badge status="missing" /></div>)}</Card>
        <Card><h2>Failed agents & budgets</h2>{data.failed_agents.map((run) => <div className="operator-row" key={run.id}><span><Link className="entity-link" to={`/agent-runs/${run.id}`}>{run.id}</Link><small>{run.terminal_error || "Run failed"}</small></span><Badge status="failed" /></div>)}{data.budgets.map((budget) => <div className="operator-row" key={budget.agent_id}><span><strong>{budget.agent_name}</strong><small>${(budget.spent_microusd / 1_000_000).toFixed(2)} spent · ${(budget.reserved_microusd / 1_000_000).toFixed(2)} reserved</small></span><span>${(budget.limit_microusd / 1_000_000).toFixed(2)}</span></div>)}</Card>
      </div>
      <Card className="table-card"><div className="card-heading operator-heading"><h2>Action ledger</h2><span className="muted">MCP latency total {data.mcp_latency_ms} ms</span></div><div className="table-wrap"><table><thead><tr><th>Action</th><th>Decision</th><th>Run</th><th>Latency</th><th>Time</th></tr></thead><tbody>{data.recent_actions.map((action) => <tr id={`action-${action.ID}`} key={action.ID}><td><strong>{action.Tool}</strong><small>{action.ID}</small></td><td><Badge status={action.PolicyDecision === "denied" ? "failed" : action.ExecutionState} /></td><td>{action.AgentRunID ? <Link className="entity-link" to={`/agent-runs/${action.AgentRunID}`}>{action.AgentRunID}</Link> : "—"}</td><td>{action.LatencyMS} ms</td><td>{fmtTime(action.CreatedAt)}</td></tr>)}</tbody></table></div></Card>
    </>}
  </>;
}
