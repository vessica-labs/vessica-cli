import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { Agents } from "./agents";

const envelope = (data: unknown) => ({ schema: "vessica.dashboard/v1", data });
let created = false;
const server = setupServer(
  http.get("/api/v1/system", () =>
    HttpResponse.json(
      envelope({
        agent_runtime: { connected: true, credentials_ready: false },
      }),
    ),
  ),
  http.get("/api/v1/agents", () => HttpResponse.json(envelope({ agents: [] }))),
  http.post("/api/v1/agent-builds", async ({ request }) => {
    const body = (await request.json()) as {
      description: string;
      review: boolean;
    };
    created = body.description === "Research product changes" && body.review;
    return HttpResponse.json(envelope({ id: "abuild_1", status: "queued" }));
  }),
);

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  created = false;
});
afterAll(() => server.close());

describe("Agents management", () => {
  it("shows credential setup and submits a reviewed natural-language build", async () => {
    render(
      <QueryClientProvider
        client={
          new QueryClient({
            defaultOptions: {
              queries: { retry: false },
              mutations: { retry: false },
            },
          })
        }
      >
        <MemoryRouter>
          <Agents />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByText("OpenAI credentials required"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("ves auth login openai --env OPENAI_API_KEY"),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText(/Research competitor/), {
      target: { value: "Research product changes" },
    });
    fireEvent.click(screen.getByLabelText("Review before activation"));
    fireEvent.click(screen.getByRole("button", { name: "Build agent" }));
    await waitFor(() => expect(created).toBe(true));
  });
});
