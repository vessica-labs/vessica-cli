import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api } from "@/lib/api";
import { Card, Empty, ErrorState, Loading, PageHeader } from "@/components/ui";
export function Docs() {
  const q = useQuery({
    queryKey: ["docs"],
    queryFn: () => api<any[]>("/api/v1/docs"),
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return (
    <>
      <PageHeader
        eyebrow="Offline and version matched"
        title="Documentation"
        description="Guidance packaged with this exact Vessica binary."
      />
      <div className="card-grid docs-grid">
        {q.data?.map((d) => (
          <Link to={`/docs/${d.slug}`} key={d.slug}>
            <Card>
              <p className="eyebrow">Guide</p>
              <h2>{d.title}</h2>
              <p>{Math.round(d.bytes / 1024)} KB · available offline</p>
            </Card>
          </Link>
        ))}
        {!q.data?.length && (
          <Card>
            <Empty
              title="No embedded documentation"
              detail="Rebuild the dashboard documentation bundle."
            />
          </Card>
        )}
      </div>
    </>
  );
}
export function Doc() {
  const { slug = "" } = useParams();
  const q = useQuery({
    queryKey: ["doc", slug],
    queryFn: () => api<{ markdown: string }>(`/api/v1/docs/${slug}`),
  });
  if (q.isLoading) return <Loading />;
  if (q.error) return <ErrorState error={q.error} />;
  return (
    <Card className="document">
      <div className="markdown">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>
          {q.data?.markdown}
        </ReactMarkdown>
      </div>
    </Card>
  );
}
