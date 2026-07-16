import { useQuery } from "@tanstack/react-query";
import { Brain, Cloud, GitBranch } from "lucide-react";
import { api, titleCase } from "@/lib/api";
import { Badge, Card, ErrorState, Loading, PageHeader } from "@/components/ui";

type Repository = { id: string; display_name: string; canonical_remote: string; status: string };
type System = { workspace_id: string; mode: string; knowledge: Record<string, any>; repositories: Repository[] };

export function Workspace() {
  const query = useQuery({ queryKey:["workspace"], queryFn:()=>api<System>("/api/v1/system"), refetchInterval:15000 });
  if (query.isLoading) return <Loading label="Reading hosted workspace" />;
  if (query.error) return <ErrorState error={query.error} />;
  const system=query.data!;
  const lexical=system.knowledge.retrieval_mode==="lexical";
  return <>
    <PageHeader eyebrow="Hosted authority" title="Vessica workspace" description="One durable Railway-hosted workspace shared by every attached repository." />
    <div className="two-column">
      <Card><Cloud className="feature-icon"/><h2>Workspace</h2><p><code>{system.workspace_id}</code></p><Badge status={system.mode}/></Card>
      <Card><Brain className="feature-icon"/><h2>Knowledge retrieval</h2><p><strong>{titleCase(system.knowledge.retrieval_mode || "lexical")}</strong> · {titleCase(system.knowledge.embedding_state || "not configured")}</p>{lexical&&<div className="notice"><strong>Healthy without an API key</strong><span>Semantic retrieval is optional. Enable it from a trusted terminal with <code>ves knowledge embeddings enable --api-key-env OPENAI_API_KEY --yes</code>.</span></div>}</Card>
    </div>
    <Card><div className="section-heading"><div><p className="eyebrow">Repositories</p><h2>Attached repositories</h2></div><Badge status="ready"/></div><div className="integration-list">{system.repositories?.map(repo=><div key={repo.id}><GitBranch size={18}/><span><strong>{repo.display_name}</strong><small>{repo.canonical_remote}</small></span><Badge status={repo.status}/></div>)}</div>{!system.repositories?.length&&<p className="muted">Run <code>ves up</code> from a repository to attach it.</p>}</Card>
  </>;
}
