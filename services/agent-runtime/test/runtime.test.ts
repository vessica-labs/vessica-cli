import { afterEach, describe, expect, it, vi } from "vitest";
import { Runtime } from "../src/runtime.js";
import type { Executor } from "../src/executor.js";
import type { ClaimedTask } from "../src/contracts.js";
import type { ControlPlaneClient } from "../src/control-plane.js";

const task: ClaimedTask = {
  protocol: "vessica.agent-runtime/v1", fence_token: "fence_1",
  task: { id: "atask_1", kind: "run", subject_id: "arun_1", attempts: 1 },
  run: { id: "arun_1", input_json: '{"prompt":"hello"}', trigger: "manual", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
  definition: {
    kind: "vessica.agent/v1", name: "TEST", purpose: "Test", system_prompt: "Help",
    model: { id: "gpt-5.6-terra", reasoning_effort: "medium" }, tools: [], knowledge: [],
    heartbeat: null, budget: { daily_usd: "5.00", timezone: "UTC" }, eval_critic_agent_id: null,
  },
};

describe("Runtime", () => {
  afterEach(() => vi.useRealTimers());
  it("completes a claimed run and reports usage", async () => {
    process.env.OPENAI_API_KEY = "test-only";
    const client = {
      heartbeat: vi.fn().mockResolvedValue({ cancel_requested: false }),
      complete: vi.fn().mockResolvedValue({}), fail: vi.fn().mockResolvedValue({}),
    } as unknown as ControlPlaneClient;
    const usage = { requests: 1, input_tokens: 10, cached_input_tokens: 0, output_tokens: 5, reasoning_tokens: 1, total_tokens: 15, response_ids: ["resp_1"] };
    const executor: Executor = { build: vi.fn(), run: vi.fn().mockResolvedValue({ output: "done", usage, cost: 42 }) };
    const runtime = new Runtime(client, executor);
    await runtime.execute(task);
    expect(client.complete).toHaveBeenCalledWith("arun_1", "fence_1", "done", usage, 42);
    expect(client.fail).not.toHaveBeenCalled();
  });

  it("reports a failed run without exposing an exception object", async () => {
    const client = { heartbeat: vi.fn(), complete: vi.fn(), fail: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const executor: Executor = { build: vi.fn(), run: vi.fn().mockRejectedValue(new Error("model unavailable")) };
    await new Runtime(client, executor).execute(task);
    expect(client.fail).toHaveBeenCalledWith("arun_1", "fence_1", "model unavailable", expect.any(Object), 0);
  });

  it("returns failed builder work to the durable retry protocol", async () => {
    const buildTask = { ...task, task: { ...task.task, kind: "build" as const, subject_id: "abuild_1" }, build: { id: "abuild_1", kind: "create" as const, description: "build it" }, run: undefined, definition: undefined };
    const client = { heartbeat: vi.fn(), failTask: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const executor: Executor = { build: vi.fn().mockRejectedValue(new Error("invalid structured output")), run: vi.fn() };
    await new Runtime(client, executor).execute(buildTask);
    expect(client.failTask).toHaveBeenCalledWith("atask_1", "fence_1", "invalid structured output");
  });

  it("cancels an in-flight model call after the control plane requests cancellation", async () => {
    vi.useFakeTimers();
    const client = {
      heartbeat: vi.fn().mockResolvedValue({ cancel_requested: true }),
      complete: vi.fn(),
      fail: vi.fn().mockResolvedValue({}),
    } as unknown as ControlPlaneClient;
    const executor: Executor = {
      build: vi.fn(),
      run: vi.fn().mockImplementation((_task: ClaimedTask, signal: AbortSignal) => new Promise((_resolve, reject) => {
        signal.addEventListener("abort", () => reject(new Error("run cancelled")), { once: true });
      })),
    };
    const execution = new Runtime(client, executor).execute(task);
    await vi.advanceTimersByTimeAsync(15_000);
    await execution;
    expect(client.fail).toHaveBeenCalledWith("arun_1", "fence_1", "run cancelled", expect.any(Object), 0);
  });
});
