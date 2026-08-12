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

  it("admits no more concurrent runs for an agent than its v2 definition allows", async () => {
    let finishFirst!: () => void;
    const first = new Promise<void>((resolve) => { finishFirst = resolve; });
    const client = { heartbeat: vi.fn(), complete: vi.fn().mockResolvedValue({}), fail: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const executor: Executor = {
      build: vi.fn(),
      run: vi.fn().mockImplementationOnce(async () => { await first; return { output: "one", usage: { requests: 0, input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, total_tokens: 0, response_ids: [] }, cost: 0 }; })
        .mockResolvedValue({ output: "two", usage: { requests: 0, input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, total_tokens: 0, response_ids: [] }, cost: 0 }),
    };
    const v2 = { ...task, run: { ...task.run!, agent_id: "agent_1" }, definition: { ...task.definition!, kind: "vessica.agent/v2" as const, runtime: { kind: "typescript_agents_sdk" as const }, action_policy: { default: "deny" as const, allowed_actions: [], approval_required: [] }, writable_knowledge_namespaces: [], sources: { network: "none" as const, allowed_domains: [], allowed_source_types: [] }, concurrency: 1, timeout_seconds: 30, conversations: { enabled: false, max_turns: 25 }, checkpoints: { enabled: false, interval_seconds: 0 } } };
    const runtime = new Runtime(client, executor, 4, true, () => ({ stop() {} }));
    const one = runtime.execute(v2);
    const two = runtime.execute({ ...v2, fence_token: "fence_2", task: { ...v2.task, id: "task_2", subject_id: "run_2" }, run: { ...v2.run, id: "run_2" } });
    await Promise.resolve();
    await Promise.resolve();
    expect(executor.run).toHaveBeenCalledTimes(1);
    finishFirst();
    await one;
    await two;
    expect(executor.run).toHaveBeenCalledTimes(2);
  });

  it("uses the v2 definition timeout", async () => {
    vi.useFakeTimers();
    const client = { heartbeat: vi.fn(), complete: vi.fn(), fail: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const executor: Executor = { build: vi.fn(), run: vi.fn().mockImplementation((_task, signal) => new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(signal.reason), { once: true }))) };
    const timed = { ...task, definition: { ...task.definition!, kind: "vessica.agent/v2" as const, runtime: { kind: "typescript_agents_sdk" as const }, action_policy: { default: "deny" as const, allowed_actions: [], approval_required: [] }, writable_knowledge_namespaces: [], sources: { network: "none" as const, allowed_domains: [], allowed_source_types: [] }, concurrency: 1, timeout_seconds: 2, conversations: { enabled: false, max_turns: 25 }, checkpoints: { enabled: false, interval_seconds: 0 } } };
    const execution = new Runtime(client, executor, 4, true, () => ({ stop() {} })).execute(timed);
    await vi.advanceTimersByTimeAsync(2_000);
    await execution;
    expect(client.fail).toHaveBeenCalledWith("arun_1", "fence_1", "run exceeded 2 second limit", expect.any(Object), 0);
  });

  it("starts lease and timeout before admission and never executes an aborted waiter", async () => {
    vi.useFakeTimers();
    let finishFirst!: () => void;
    const firstGate = new Promise<void>((resolve) => { finishFirst = resolve; });
    const aborters = new Map<string, (reason: Error) => void>();
    const stopped: string[] = [];
    const leaseFactory = vi.fn((_client, claimed: ClaimedTask, onAbort: (reason: Error) => void) => {
      aborters.set(claimed.task.subject_id, onAbort);
      return { stop: () => stopped.push(claimed.task.subject_id) };
    });
    const client = { complete: vi.fn().mockResolvedValue({}), fail: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const executor: Executor = {
      build: vi.fn(),
      run: vi.fn().mockImplementationOnce(async () => { await firstGate; return { output: "one", usage: { requests: 0, input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, total_tokens: 0, response_ids: [] }, cost: 0 }; }),
    };
    const v2 = { ...task, run: { ...task.run!, agent_id: "agent_wait" }, definition: { ...task.definition!, kind: "vessica.agent/v2" as const, runtime: { kind: "typescript_agents_sdk" as const }, action_policy: { default: "deny" as const, allowed_actions: [], approval_required: [] }, writable_knowledge_namespaces: [], sources: { network: "none" as const, allowed_domains: [], allowed_source_types: [] }, concurrency: 1, timeout_seconds: 30, conversations: { enabled: false, max_turns: 25 }, checkpoints: { enabled: false, interval_seconds: 0 } } };
    const runtime = new Runtime(client, executor, 4, true, leaseFactory);
    const first = runtime.execute(v2);
    const waiting = runtime.execute({ ...v2, fence_token: "fence_wait", task: { ...v2.task, id: "task_wait", subject_id: "run_wait" }, run: { ...v2.run, id: "run_wait" } });
    await vi.waitFor(() => expect(leaseFactory).toHaveBeenCalledTimes(2));
    expect(executor.run).toHaveBeenCalledTimes(1);
    aborters.get("run_wait")?.(new Error("attempt lease lost"));
    await waiting;
    finishFirst();
    await first;
    expect(executor.run).toHaveBeenCalledTimes(1);
    expect(client.fail).toHaveBeenCalledWith("run_wait", "fence_wait", "attempt lease lost", expect.any(Object), 0);
    expect(stopped).toEqual(expect.arrayContaining(["run_wait", "arun_1"]));
  });

  it("retains v1 immediate admission while starting its lease before execution", async () => {
    const order: string[] = [];
    const client = { complete: vi.fn().mockResolvedValue({}), fail: vi.fn() } as unknown as ControlPlaneClient;
    const executor: Executor = { build: vi.fn(), run: vi.fn().mockImplementation(async () => { order.push("execute"); return { output: "done", usage: { requests: 0, input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, total_tokens: 0, response_ids: [] }, cost: 0 }; }) };
    const runtime = new Runtime(client, executor, 4, true, () => { order.push("lease"); return { stop() {} }; });
    await Promise.all([runtime.execute(task), runtime.execute({ ...task, task: { ...task.task, id: "v1_2", subject_id: "v1_run_2" }, run: { ...task.run!, id: "v1_run_2" } })]);
    expect(order.filter((value) => value === "lease")).toHaveLength(2);
    expect(executor.run).toHaveBeenCalledTimes(2);
  });
});
