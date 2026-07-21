import type { ClaimedTask, NormalizedEvent, RuntimeCapabilities, Usage } from "./contracts.js";

export class ControlPlaneClient {
  constructor(private readonly baseURL: string, private readonly token: string) {}

  runtimeConnection() {
    return { base_url: this.baseURL, token: this.token };
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const response = await fetch(`${this.baseURL}${path}`, {
      ...init,
      headers: {
        authorization: `Bearer ${this.token}`,
        "content-type": "application/json",
        ...init.headers,
      },
    });
    if (response.status === 204) return undefined as T;
    const text = await response.text();
    if (!response.ok) throw new Error(`control plane ${response.status}: ${text.slice(0, 512)}`);
    return (text ? JSON.parse(text) : undefined) as T;
  }

  capabilities(value: RuntimeCapabilities) {
    return this.request("/internal/agent-runtime/v1/capabilities", { method: "POST", body: JSON.stringify(value) });
  }
  claim(workerID: string, capabilities: RuntimeCapabilities) {
    return this.request<ClaimedTask | undefined>("/internal/agent-runtime/v1/tasks/claim", { method: "POST", body: JSON.stringify({ worker_id: workerID, capabilities }) });
  }
  heartbeat(taskID: string, subjectID: string, fence: string) {
    return this.request<{ cancel_requested: boolean }>(`/internal/agent-runtime/v1/tasks/${taskID}/heartbeat`, { method: "POST", body: JSON.stringify({ subject_id: subjectID, fence_token: fence }) });
  }
  failTask(taskID: string, fence: string, error: string) {
    return this.request(`/internal/agent-runtime/v1/tasks/${taskID}/fail`, { method: "POST", body: JSON.stringify({ fence_token: fence, error }) });
  }
  completeBuild(id: string, fence: string, definition: unknown, warnings: string[], usage: unknown) {
    return this.request(`/internal/agent-runtime/v1/builds/${id}/complete`, { method: "POST", body: JSON.stringify({ fence_token: fence, definition, warnings, usage }) });
  }
  events(runID: string, fence: string, events: NormalizedEvent[]) {
    return this.request(`/internal/agent-runtime/v1/runs/${runID}/events`, { method: "POST", body: JSON.stringify({ fence_token: fence, events }) });
  }
  usage(runID: string, fence: string, usage: Usage) {
    return this.request(`/internal/agent-runtime/v1/runs/${runID}/usage`, { method: "POST", body: JSON.stringify({ fence_token: fence, usage }) });
  }
  complete(runID: string, fence: string, output: unknown, usage: Usage, actualMicroUSD: number) {
    return this.request(`/internal/agent-runtime/v1/runs/${runID}/complete`, { method: "POST", body: JSON.stringify({ fence_token: fence, output, usage, actual_microusd: actualMicroUSD }) });
  }
  fail(runID: string, fence: string, error: string, usage: Usage, actualMicroUSD: number) {
    return this.request(`/internal/agent-runtime/v1/runs/${runID}/fail`, { method: "POST", body: JSON.stringify({ fence_token: fence, error, usage, actual_microusd: actualMicroUSD }) });
  }
  tool(runID: string, fence: string, toolID: string, ordinal: number, args: unknown) {
    return this.request<{ result: unknown }>(`/internal/agent-runtime/v1/runs/${runID}/tools/${encodeURIComponent(toolID)}`, { method: "POST", body: JSON.stringify({ fence_token: fence, ordinal, arguments: args }) });
  }
  child(runID: string, fence: string, agent: string, prompt: string) {
    return this.request<{ child: { id: string; status: string }; execution?: ClaimedTask }>(`/internal/agent-runtime/v1/runs/${runID}/children`, { method: "POST", body: JSON.stringify({ fence_token: fence, agent, prompt }) });
  }
}
