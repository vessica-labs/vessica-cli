import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { CitationLink, KnowledgeDetail } from "./knowledge";

const envelope = (data: unknown) => ({ schema: "vessica.dashboard/v1", data });
const server = setupServer(
  http.get("/api/v1/knowledge/memories/mem_1", () =>
    HttpResponse.json(
      envelope({
        id: "mem_1",
        scope_id: "scope_1",
        version: 1,
        type: "fact",
        title: "Imported memory",
        content: "Remember this.",
        state: "active",
      }),
    ),
  ),
  http.get("/api/v1/knowledge/relationships", () =>
    HttpResponse.json(envelope({ items: null })),
  ),
  http.get("/api/v1/knowledge/memories/mem_1/versions", () =>
    HttpResponse.json(envelope({ items: null })),
  ),
);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  cleanup();
  server.resetHandlers();
});
afterAll(() => server.close());

describe("Knowledge detail", () => {
  it("renders when empty relationship and version collections are null", async () => {
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: { queries: { retry: false } },
          })
        }
      >
        <MemoryRouter initialEntries={["/knowledge/memory/mem_1"]}>
          <Routes>
            <Route path="/knowledge/:type/:id" element={<KnowledgeDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("Imported memory")).toBeInTheDocument();
    expect(screen.getByText("No relationships recorded.")).toBeInTheDocument();
    expect(screen.getByText("Immutable versions")).toBeInTheDocument();
  });

  it.each([
    ["artifact", "art_123", "/api/v1/knowledge/artifacts/art_123"],
    ["memory", "mem_123", "/api/v1/knowledge/memories/mem_123"],
    ["entity", "ent_123", "/api/v1/knowledge/entities/ent_123"],
  ])(
    "routes an exact %s citation to its detail API",
    async (type, id, expectedAPI) => {
      let requested = "";
      server.use(
        http.get(expectedAPI, ({ request }) => {
          requested = new URL(request.url).pathname;
          return HttpResponse.json(
            envelope({
              id,
              title: `Evidence ${id}`,
              scope_id: "scope_1",
              version: 1,
              state: "active",
              content: "Exact evidence",
            }),
          );
        }),
        http.get("/api/v1/knowledge/relationships", () =>
          HttpResponse.json(envelope({ items: [] })),
        ),
        http.get(`${expectedAPI}/versions`, () =>
          HttpResponse.json(envelope({ items: [] })),
        ),
      );
      render(
        <QueryClientProvider
          client={
            new QueryClient({ defaultOptions: { queries: { retry: false } } })
          }
        >
          <MemoryRouter initialEntries={["/source"]}>
            <Routes>
              <Route path="/source" element={<CitationLink citation={id} />} />
              <Route
                path="/knowledge/:type/:id"
                element={<KnowledgeDetail />}
              />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      );
      fireEvent.click(screen.getByRole("link", { name: id }));
      expect(await screen.findByText(`Evidence ${id}`)).toBeInTheDocument();
      expect(requested).toBe(expectedAPI);
    },
  );

  it("shows the exact API error when a valid citation ID is missing", async () => {
    server.use(
      http.get("/api/v1/knowledge/artifacts/art_missing", () =>
        HttpResponse.json(
          {
            schema: "vessica.dashboard/v1",
            error: {
              code: "not_found",
              message: "Cited artifact not found",
              request_id: "req_missing",
            },
          },
          { status: 404 },
        ),
      ),
      http.get("/api/v1/knowledge/relationships", () =>
        HttpResponse.json(envelope({ items: [] })),
      ),
      http.get("/api/v1/knowledge/artifacts/art_missing/versions", () =>
        HttpResponse.json(envelope({ items: [] })),
      ),
    );
    render(
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter initialEntries={["/source"]}>
          <Routes>
            <Route
              path="/source"
              element={<CitationLink citation="art_missing" />}
            />
            <Route path="/knowledge/:type/:id" element={<KnowledgeDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole("link", { name: "art_missing" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Cited artifact not found",
    );
  });

  it("does not create a route for an invalid citation ID", () => {
    render(
      <MemoryRouter>
        <CitationLink citation="newsletter-source-1" />
      </MemoryRouter>,
    );
    expect(screen.getByText("newsletter-source-1")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
