import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Search } from "lucide-react";
import { api, fmtTime, titleCase } from "@/lib/api";
import {
  Badge,
  Card,
  Empty,
  ErrorState,
  Loading,
  PageHeader,
} from "@/components/ui";
export function Knowledge() {
  const [q, setQ] = useState("");
  const [type, setType] = useState("");
  const result = useQuery({
    queryKey: ["knowledge", q, type],
    queryFn: () =>
      api<{ items: any[] }>(
        `/api/v1/knowledge/search?q=${encodeURIComponent(q)}&object_type=${type}&limit=100`,
      ),
  });
  const status = useQuery({
    queryKey: ["knowledge-status"],
    queryFn: () => api<any>("/api/v1/knowledge/status"),
  });
  return (
    <>
      <PageHeader
        eyebrow="Authoritative context"
        title="Knowledge explorer"
        description="Trace entities, artifacts, memories, relationships, versions, and retrieval provenance."
        actions={
          status.data && (
            <Badge status={status.data.index_fresh ? "ready" : "catching_up"} />
          )
        }
      />
      <Card>
        <div className="search-row">
          <Search size={18} />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search repositories, decisions, facts, and episodes…"
          />
          <select value={type} onChange={(e) => setType(e.target.value)}>
            <option value="">All objects</option>
            <option value="entity">Entities</option>
            <option value="artifact">Artifacts</option>
            <option value="memory">Memories</option>
          </select>
        </div>
        <p className="retrieval-note">
          {titleCase(status.data?.retrieval_mode)} retrieval ·{" "}
          {status.data?.embedding_model || "no embedding model configured"}
        </p>
      </Card>
      {result.isLoading ? (
        <Loading />
      ) : result.error ? (
        <ErrorState error={result.error} />
      ) : (
        <div className="result-list">
          {result.data?.items.map((item) => (
            <Link
              to={`/knowledge/${item.object_type}/${item.id}`}
              key={`${item.object_type}-${item.id}`}
              className="result-item"
            >
              <span className="object-type">
                {item.object_type.slice(0, 1).toUpperCase()}
              </span>
              <div>
                <strong>{item.title || item.id}</strong>
                <p>
                  {item.summary ||
                    `${titleCase(item.subtype)} knowledge object`}
                </p>
                <small>
                  {titleCase(item.subtype)} · {fmtTime(item.updated_at)}
                </small>
              </div>
              <Badge status={item.state} />
            </Link>
          ))}
          {result.data?.items.length === 0 && (
            <Card>
              <Empty
                title="No matching knowledge"
                detail="Try a broader query or remove the object filter."
              />
            </Card>
          )}
        </div>
      )}
    </>
  );
}
export function KnowledgeDetail() {
  const { type = "", id = "" } = useParams();
  const path =
    type === "entity"
      ? `entities/${id}`
      : type === "artifact"
        ? `artifacts/${id}`
        : `memories/${id}`;
  const detail = useQuery({
    queryKey: ["knowledge-detail", type, id],
    queryFn: () => api<any>(`/api/v1/knowledge/${path}`),
  });
  const relationships = useQuery({
    queryKey: ["relationships", id],
    queryFn: () =>
      api<{ items: any[] }>(`/api/v1/knowledge/relationships?object_id=${id}`),
  });
  const versions = useQuery({
    queryKey: ["versions", type, id],
    queryFn: () =>
      type === "entity"
        ? Promise.resolve({ items: [] })
        : api<{ items: any[] }>(`/api/v1/knowledge/${path}/versions`),
  });
  if (detail.isLoading) return <Loading />;
  if (detail.error) return <ErrorState error={detail.error} />;
  const v = detail.data;
  return (
    <>
      <PageHeader
        eyebrow={titleCase(type)}
        title={v.title || v.display_name || v.id}
        description={`Scope ${v.scope_id} · version ${v.version}`}
        actions={<Badge status={v.state || v.status} />}
      />
      <div className="two-column detail-columns">
        <Card>
          <h2>Content</h2>
          {v.content ? (
            <div className="markdown">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {v.content}
              </ReactMarkdown>
            </div>
          ) : (
            <pre className="json-block">
              {JSON.stringify(v.metadata, null, 2)}
            </pre>
          )}
          <h3>Provenance</h3>
          <pre className="json-block">
            {JSON.stringify(v.provenance, null, 2)}
          </pre>
        </Card>
        <div>
          <Card>
            <h2>Relationships</h2>
            {relationships.data?.items.map((r) => (
              <div className="relationship" key={r.id}>
                <Badge status={r.state} />
                <span>
                  {r.from_id} <strong>{r.predicate}</strong> {r.to_id}
                </span>
              </div>
            ))}
            {!relationships.data?.items.length && (
              <p className="muted">No relationships recorded.</p>
            )}
          </Card>
          {type !== "entity" && (
            <Card>
              <h2>Immutable versions</h2>
              {versions.data?.items.map((item) => (
                <div className="version-row" key={item.version}>
                  <strong>v{item.version}</strong>
                  <span>{fmtTime(item.updated_at)}</span>
                  <Badge status={item.state || item.status} />
                </div>
              ))}
            </Card>
          )}
        </div>
      </div>
    </>
  );
}
