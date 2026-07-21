import type { ControlPlaneClient } from "./control-plane.js";
import { DEFAULT_MODEL, type AgentDefinition, type ClaimedTask, type Usage } from "./contracts.js";
import type { Executor } from "./executor.js";

const usage = (runID: string): Usage => ({
  requests: 1,
  input_tokens: 100,
  cached_input_tokens: 0,
  output_tokens: 30,
  reasoning_tokens: 5,
  total_tokens: 130,
  response_ids: [`resp_fake_${runID}`],
});

function extracted(description: string, pattern: RegExp) {
  return description.match(pattern)?.[1]?.trim();
}

async function validationDelay(signal?: AbortSignal) {
  const delay = Math.min(Math.max(Number(process.env.VES_AGENT_RUNTIME_FAKE_DELAY_MS || 0), 0), 300_000);
  if (!delay) return;
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(resolve, delay);
    signal?.addEventListener("abort", () => { clearTimeout(timer); reject(signal.reason ?? new Error("run cancelled")); }, { once: true });
  });
}

// FakeExecutor is deliberately opt-in and deterministic. It exercises the
// durable runtime protocol for CI and release smoke tests without sending an
// OpenAI request or weakening the production credential boundary.
export class FakeExecutor implements Executor {
  constructor(private readonly client: ControlPlaneClient) {}

  async build(task: ClaimedTask, signal?: AbortSignal) {
    if (!task.build) throw new Error("builder task is incomplete");
    await validationDelay(signal);
    const description = task.build.description;
    const name = extracted(description, /\bname(?:d)?\s+([A-Za-z0-9_-]+)/i) ?? "VALIDATOR";
    const critic = extracted(description, /\bcritic\s+(agent_[A-Za-z0-9]+)/i) ?? null;
    const definition: AgentDefinition = {
      kind: "vessica.agent/v1",
      name,
      purpose: `Deterministic validation agent generated for: ${description.slice(0, 240)}`,
      system_prompt: "Return a concise deterministic validation response. Do not call external tools.",
      model: { id: DEFAULT_MODEL, reasoning_effort: "medium" },
      tools: [],
      knowledge: [],
      heartbeat: null,
      budget: { daily_usd: "1.00", timezone: task.client_timezone || "UTC" },
      eval_critic_agent_id: critic,
    };
    return { definition, warnings: ["fake provider enabled for validation"], usage: usage(task.task.subject_id) };
  }

  async run(task: ClaimedTask, signal: AbortSignal) {
    if (!task.run) throw new Error("run task is incomplete");
    if (signal.aborted) throw new Error("run cancelled");
    await validationDelay(signal);
    const evaluated = task.task.kind === "eval";
    const output = evaluated
      ? { score: 0.97, passed: true, summary: "Deterministic smoke evaluation passed.", findings: [{ severity: "info", message: "Durable agent output was present and reviewable." }] }
      : `Deterministic validation response for ${task.run.id}.`;
    const runUsage = usage(task.run.id);
    await this.client.events(task.run.id, task.fence_token, [
      { ordinal: 1, type: "agent.run.started", payload: { attempt: task.attempt?.attempt_number ?? 1, trigger: task.run.trigger, provider: "fake" } },
      { ordinal: 2, type: "agent.message.delta", payload: { text: typeof output === "string" ? output : output.summary } },
      { ordinal: 3, type: "agent.message.completed", payload: { text: typeof output === "string" ? output : output.summary } },
      { ordinal: 4, type: "agent.usage", payload: runUsage },
      ...(evaluated ? [{ ordinal: 5, type: "agent.eval.completed", payload: output }] : []),
      { ordinal: evaluated ? 6 : 5, type: "agent.run.completed", payload: { response_ids: runUsage.response_ids } },
    ]);
    await this.client.usage(task.run.id, task.fence_token, runUsage);
    return { output, usage: runUsage, cost: 425 };
  }
}
