import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter } from "react-router-dom";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { Conversations } from "@/pages/conversations";

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

describe("Conversations", () => {
  it("shows an actionable error without hiding the conversation surface", async () => {
    server.use(
      http.get("/api/v1/conversations", () =>
        HttpResponse.json(
          { schema: "vessica.dashboard/v1", error: { code: "unavailable", message: "Conversation service unavailable", request_id: "req_1" } },
          { status: 503 },
        ),
      ),
      http.get("/api/v1/agents", () => HttpResponse.json({ schema: "vessica.dashboard/v1", data: { agents: [] } })),
    );
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter><Conversations /></MemoryRouter>
      </QueryClientProvider>,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Conversation service unavailable");
    expect(screen.getByRole("heading", { name: "Conversations" })).toBeInTheDocument();
    expect(screen.getByText("Start a conversation")).toBeInTheDocument();
  });
});

