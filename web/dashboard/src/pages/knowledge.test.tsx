import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { KnowledgeDetail } from "./knowledge";

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
afterEach(() => server.resetHandlers());
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
});
