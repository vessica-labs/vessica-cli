import { useQuery } from "@tanstack/react-query";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Link } from "react-router-dom";
import { api, fmtTime } from "@/lib/api";
import { Card, Empty, ErrorState, Loading, PageHeader } from "@/components/ui";
import { CitationLink } from "./knowledge";

type Artifact = {
  id: string;
  type: string;
  title: string;
  content: string;
  updated_at: string;
  metadata?: { citations?: string[]; slot?: string; generated_at?: string };
};

export function Briefings() {
  const query = useQuery({
    queryKey: ["briefings"],
    queryFn: () =>
      api<{ briefings: Artifact[]; newsletters: Artifact[] }>(
        "/api/v1/briefings",
      ),
    refetchInterval: 60_000,
  });
  const sections: Array<[string, Artifact[] | undefined]> = [
    ["COS briefings", query.data?.briefings],
    ["Daily newsletter", query.data?.newsletters],
  ];
  return (
    <>
      <PageHeader
        eyebrow="Daily intelligence"
        title="Briefings & newsletter"
        description="Versioned knowledge artifacts with generation freshness and source citations."
      />
      {query.isLoading && <Loading label="Loading briefings" />}
      {query.error && <ErrorState error={query.error} />}
      {query.data && (
        <div className="briefing-grid">
          {sections.map(([title, items]) => (
            <section key={title}>
              <h2>{title}</h2>
              {!items?.length && (
                <Card>
                  <Empty
                    title={`No ${title.toLowerCase()}`}
                    detail="The operator view shows the missing schedule or source checkpoint."
                  />
                </Card>
              )}
              {items?.map((item) => (
                <Card key={item.id} className="briefing-card">
                  <div className="card-heading">
                    <div>
                      <h3>{item.title}</h3>
                      <p className="muted">
                        Updated {fmtTime(item.updated_at)}
                      </p>
                    </div>
                    <Link
                      className="entity-link"
                      to={`/knowledge/artifact/${item.id}`}
                    >
                      Artifact
                    </Link>
                  </div>
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>
                    {item.content}
                  </ReactMarkdown>
                  {!!item.metadata?.citations?.length && (
                    <div className="citation-list">
                      <strong>Citations</strong>
                      {item.metadata.citations.map((citation) => (
                        <CitationLink key={citation} citation={citation} />
                      ))}
                    </div>
                  )}
                </Card>
              ))}
            </section>
          ))}
        </div>
      )}
    </>
  );
}
