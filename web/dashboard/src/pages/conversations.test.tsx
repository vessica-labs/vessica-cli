import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { setupServer } from "msw/node";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { Conversations } from "@/pages/conversations";

const server = setupServer();
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => { cleanup(); server.resetHandlers(); });
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

  it("reuses the message idempotency key after an ambiguous network failure", async () => {
	const keys: string[] = [];
	let attempts = 0;
	server.use(
	  http.get("/api/v1/conversations", () => HttpResponse.json({ schema: "vessica.dashboard/v1", data: { conversations: [{ id: "conv_1", agent_id: "agent_1", title: "Priorities", status: "active", updated_at: "2026-08-12T00:00:00Z" }] } })),
	  http.get("/api/v1/agents", () => HttpResponse.json({ schema: "vessica.dashboard/v1", data: { agents: [{ id: "agent_1", name: "COS", state: "active" }] } })),
	  http.get("/api/v1/conversations/conv_1", () => HttpResponse.json({ schema: "vessica.dashboard/v1", data: { conversation: { id: "conv_1", agent_id: "agent_1", title: "Priorities", status: "active", updated_at: "2026-08-12T00:00:00Z" }, messages: [], runs: [] } })),
	  http.post("/api/v1/conversations/conv_1/messages", ({ request }) => {
		keys.push(request.headers.get("Idempotency-Key") || "");
		attempts++;
		if (attempts === 1) return HttpResponse.error();
		return HttpResponse.json({ schema: "vessica.dashboard/v1", data: { message: { id: "msg_1" } } });
	  }),
	);
	render(
	  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })}>
		<MemoryRouter initialEntries={["/conversations/conv_1"]}><Routes><Route path="/conversations/:id" element={<Conversations />} /></Routes></MemoryRouter>
	  </QueryClientProvider>,
	);
	const composer = await screen.findByPlaceholderText("Ask this agent…");
	fireEvent.change(composer, { target: { value: "What changed?" } });
	fireEvent.click(screen.getByRole("button", { name: "Send" }));
	await screen.findByRole("alert");
	fireEvent.click(screen.getByRole("button", { name: "Send" }));
	await waitFor(() => expect(attempts).toBe(2));
	expect(keys[0]).toBeTruthy();
	expect(keys[1]).toBe(keys[0]);
  });
});
