import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const sdk = vi.hoisted(() => ({
  agents: [] as Array<Record<string, unknown>>,
  runnerConfigs: [] as Array<Record<string, unknown>>,
  run: vi.fn(),
}));

vi.mock("@openai/agents", () => ({
  Agent: class {
    constructor(config: Record<string, unknown>) { sdk.agents.push(config); }
  },
  Runner: class {
    constructor(config: Record<string, unknown>) { sdk.runnerConfigs.push(config); }
    run(...args: unknown[]) { return sdk.run(...args); }
  },
  codeInterpreterTool: (config: unknown) => ({ name: "code_interpreter", config }),
  setTracingDisabled: vi.fn(),
  tool: (config: Record<string, unknown>) => config,
  webSearchTool: (config: unknown) => ({ name: "web_search", config }),
}));

import { OpenAIAgentsExecutor } from "../src/executor.js";
import { AgentAdmission } from "../src/admission.js";
import type { AgentDefinition, ClaimedTask } from "../src/contracts.js";
import type { ControlPlaneClient } from "../src/control-plane.js";

const definition: AgentDefinition = {
  kind: "vessica.agent/v1",
  name: "RESEARCHER",
  purpose: "Research",
  system_prompt: "Research carefully.",
  model: { id: "gpt-5.6-terra", reasoning_effort: "medium" },
  tools: [],
  knowledge: [],
  heartbeat: null,
  budget: { daily_usd: "5.00", timezone: "UTC" },
  eval_critic_agent_id: null,
};

describe("OpenAIAgentsExecutor", () => {
  beforeEach(() => {
    sdk.agents.length = 0;
    sdk.runnerConfigs.length = 0;
    sdk.run.mockReset();
  });
  afterEach(() => vi.useRealTimers());

  it("preserves reasoning items across function-tool turns", () => {
    new OpenAIAgentsExecutor({} as ControlPlaneClient);

    expect(sdk.runnerConfigs).toEqual([expect.objectContaining({
      tracingDisabled: true,
      traceIncludeSensitiveData: false,
      reasoningItemIdPolicy: "preserve",
    })]);
  });

  it("builds with structured output and the supplied catalogs and timezone", async () => {
    sdk.run.mockResolvedValue({
      async *[Symbol.asyncIterator]() {},
      completed: Promise.resolve(),
      error: null,
      finalOutput: {
        definition: {
          ...definition,
          tools: [{ id: "openai.web_search", config_json: '{"search_context_size":"low","allowed_domains":["example.com"]}' }],
        },
        warnings: ["phone channels are deferred"],
      },
      runContext: { usage: { requests: 1, inputTokens: 20, outputTokens: 10, totalTokens: 30, inputTokensDetails: [], outputTokensDetails: [] } },
      rawResponses: [{ responseId: "resp_builder" }],
    });
    const executor = new OpenAIAgentsExecutor({} as ControlPlaneClient);
    const task = {
      protocol: "vessica.agent-runtime/v1",
      fence_token: "fence_1",
      task: { id: "atask_1", kind: "build", subject_id: "abuild_1", attempts: 1 },
      build: { id: "abuild_1", kind: "create", description: "build a researcher" },
      client_timezone: "America/Los_Angeles",
      model_catalog: ["gpt-5.6-terra"],
      tool_catalog: ["openai.web_search"],
      agent_catalog: [{ id: "agent_1", name: "CRITIC", purpose: "Evaluates research" }],
    } satisfies ClaimedTask;

    const result = await executor.build(task);

    expect(result.definition.name).toBe("RESEARCHER");
    expect(result.definition.tools).toEqual([{ id: "openai.web_search", config: { search_context_size: "low", allowed_domains: ["example.com"] } }]);
    expect(result.usage.response_ids).toEqual(["resp_builder"]);
    expect(sdk.agents[0]?.outputType).toBeTruthy();
    expect(sdk.run.mock.calls[0]?.[1]).toContain("America/Los_Angeles");
    expect(sdk.run.mock.calls[0]?.[1]).toContain("openai.web_search");
    expect(sdk.run.mock.calls[0]?.[2]).toMatchObject({ stream: true });
  });

  it("maps hosted, fenced control-plane, and durable child tools", async () => {
    const client = {
      tool: vi.fn().mockResolvedValue({ replayed: false, result: { id: "art_1" } }),
      child: vi.fn().mockResolvedValue({ child: { id: "arun_child" } }),
    } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client);
    const append = vi.fn().mockResolvedValue(undefined);
    const context = { client, runID: "arun_parent", fence: "fence_1", toolOrdinal: 0, batcher: { append }, failedToolCallIDs: new Set<string>() };
    const configured = { ...definition, tools: [
      { id: "openai.web_search", config: { search_context_size: "low" } },
      { id: "artifact.get", config: {} },
      { id: "agent.invoke", config: {} },
    ] } satisfies AgentDefinition;
    const tools = (executor as unknown as { mapTools(d: AgentDefinition, c: typeof context): Array<Record<string, unknown>> }).mapTools(configured, context);

    const artifact = tools.find((entry) => entry.name === "artifact_get") as { execute(args: unknown): Promise<unknown> };
    await artifact.execute({ artifact_id: "art_1" });
    expect(client.tool).toHaveBeenCalledWith("arun_parent", "fence_1", "artifact.get", 1, { artifact_id: "art_1" });

    const child = tools.find((entry) => entry.name === "agent_invoke") as { execute(args: { agent: string; prompt: string }): Promise<unknown> };
    await child.execute({ agent: "CRITIC", prompt: "review this" });
    expect(client.child).toHaveBeenCalledWith("arun_parent", "fence_1", "CRITIC", "review this");
    expect(append).toHaveBeenCalledWith("agent.child.started", { run_id: "arun_child", agent: "CRITIC" });
    expect(tools.some((entry) => entry.name === "web_search")).toBe(true);
    expect(tools.find((entry) => entry.name === "web_search")?.config).toEqual({
      searchContextSize: "low",
      filters: undefined,
      externalWebAccess: undefined,
      userLocation: undefined,
    });
  });

  it("associates control-plane tool failures with the SDK call id", async () => {
    const client = {
      tool: vi.fn().mockRejectedValue(new Error("invalid memory")),
    } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client);
    const append = vi.fn().mockResolvedValue(undefined);
    const context = {
      client,
      runID: "arun_parent",
      fence: "fence_1",
      toolOrdinal: 0,
      batcher: { append },
      failedToolCallIDs: new Set<string>(),
    };
    const configured = { ...definition, tools: [{ id: "memory.get", config: {} }] } satisfies AgentDefinition;
    const tools = (executor as unknown as { mapTools(d: AgentDefinition, c: typeof context): Array<Record<string, unknown>> }).mapTools(configured, context);
    const memory = tools[0] as {
      execute(args: unknown, runContext?: unknown, details?: { toolCall?: { callId: string } }): Promise<unknown>;
    };

    await expect(memory.execute({ memory_id: "mem_missing" }, undefined, { toolCall: { callId: "call_failed" } })).rejects.toThrow("invalid memory");

    expect(context.failedToolCallIDs).toEqual(new Set(["call_failed"]));
    expect(append).toHaveBeenCalledWith("agent.tool.failed", {
      tool: "memory.get",
      call_id: "call_failed",
      message: "invalid memory",
    });
  });

  it("rejects unsupported function-tool configuration", () => {
    const executor = new OpenAIAgentsExecutor({} as ControlPlaneClient);
    const context = { client: {} as ControlPlaneClient, runID: "arun_1", fence: "fence_1", toolOrdinal: 0, batcher: { append: vi.fn() }, failedToolCallIDs: new Set<string>() };
    const configured = { ...definition, tools: [{ id: "artifact.get", config: { unexpected: true } }] } as AgentDefinition;
    expect(() => (executor as unknown as { mapTools(d: AgentDefinition, c: typeof context): unknown }).mapTools(configured, context)).toThrow();
  });

  it("normalizes generic Agents SDK streaming events and checkpoints usage", async () => {
    const usage = { requests: 1, inputTokens: 20, outputTokens: 4, totalTokens: 24, inputTokensDetails: [], outputTokensDetails: [] };
    const stream = {
      async *[Symbol.asyncIterator]() {
        yield { type: "raw_model_stream_event", data: { type: "output_text_delta", delta: "VESSICA_" } };
        yield { type: "raw_model_stream_event", data: { type: "output_text_delta", delta: "OK" } };
        yield { type: "raw_model_stream_event", data: { type: "response_done", response: { id: "resp_run" } } };
      },
      completed: Promise.resolve(),
      error: null,
      finalOutput: "VESSICA_OK",
      runContext: { usage },
    };
    sdk.run.mockResolvedValue(stream);
    const client = {
      events: vi.fn().mockResolvedValue({}),
      usage: vi.fn().mockResolvedValue({}),
    } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client);
    const task = {
      protocol: "vessica.agent-runtime/v1",
      fence_token: "fence_1",
      task: { id: "atask_1", kind: "run", subject_id: "arun_1", attempts: 1 },
      attempt: { id: "aattempt_1", attempt_number: 1 },
      run: { id: "arun_1", input_json: '{"prompt":"validate"}', trigger: "manual", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
      definition,
    } satisfies ClaimedTask;

    const result = await executor.run(task, new AbortController().signal);

    expect(result.output).toBe("VESSICA_OK");
    expect(client.usage).toHaveBeenCalledWith("arun_1", "fence_1", expect.objectContaining({ response_ids: ["resp_run"] }));
    expect(client.events).toHaveBeenCalledWith("arun_1", "fence_1", expect.arrayContaining([
      { ordinal: 2, type: "agent.message.delta", payload: { text: "VESSICA_OK" } },
      { ordinal: 3, type: "agent.usage", payload: expect.objectContaining({ response_ids: ["resp_run"] }) },
      { ordinal: 4, type: "agent.message.completed", payload: { text: "VESSICA_OK" } },
    ]));
  });

  it("passes the complete durable newsletter and COS envelopes to the model", async () => {
    const stream = () => ({
      async *[Symbol.asyncIterator]() {}, completed: Promise.resolve(), error: null, finalOutput: "done",
      runContext: { usage: { requests: 0, inputTokens: 0, outputTokens: 0, totalTokens: 0, inputTokensDetails: [], outputTokensDetails: [] } },
    });
    sdk.run.mockImplementation(async () => stream());
    const client = { events: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client);
    const envelopes = [
      { prompt: "synthesize", date: "2026-08-12", items: [{ source_item_id: "item-1" }], output_contract: { title: "string" } },
      { prompt: "brief", batch_id: "batch-1", coverage: { count: 3, newest_source_at: "2026-08-12T10:00:00Z" } },
    ];
    for (const [index, envelope] of envelopes.entries()) {
      await executor.run({
        protocol: "vessica.agent-runtime/v1", fence_token: `fence_${index}`,
        task: { id: `task_${index}`, kind: "run", subject_id: `run_${index}`, attempts: 1 },
        run: { id: `run_${index}`, agent_id: "agent_1", input_json: JSON.stringify(envelope), trigger: "scheduled", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
        definition,
      }, new AbortController().signal);
      expect(JSON.parse(sdk.run.mock.calls[index]?.[1] as string)).toEqual(envelope);
    }
  });

  it("emits v2 checkpoint lifecycle events at the configured interval", async () => {
    vi.useFakeTimers();
    let finish!: () => void;
    const gate = new Promise<void>((resolve) => { finish = resolve; });
    sdk.run.mockResolvedValue({
      async *[Symbol.asyncIterator]() { await gate; }, completed: gate, error: null, finalOutput: "done",
      runContext: { usage: { requests: 0, inputTokens: 0, outputTokens: 0, totalTokens: 0, inputTokensDetails: [], outputTokensDetails: [] } },
    });
    const client = { events: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client);
    const run = executor.run({
      protocol: "vessica.agent-runtime/v1", fence_token: "fence_checkpoint",
      task: { id: "task_checkpoint", kind: "run", subject_id: "run_checkpoint", attempts: 1 },
      run: { id: "run_checkpoint", agent_id: "agent_checkpoint", input_json: '{"prompt":"wait"}', trigger: "manual", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
      definition: { ...definition, kind: "vessica.agent/v2", runtime: { kind: "typescript_agents_sdk" }, action_policy: { default: "deny", allowed_actions: [], approval_required: [] }, writable_knowledge_namespaces: [], sources: { network: "none", allowed_domains: [], allowed_source_types: [] }, concurrency: 1, timeout_seconds: 30, conversations: { enabled: false, max_turns: 25 }, checkpoints: { enabled: true, interval_seconds: 5 } },
    }, new AbortController().signal);
    await vi.advanceTimersByTimeAsync(5_000);
    const emittedTypes = () => client.events.mock.calls.flatMap((call) => call[2]).map((event) => event.type);
    expect(emittedTypes()).toEqual(expect.arrayContaining(["agent.checkpoint.started", "agent.checkpoint.saved"]));
    finish();
    await run;
    await vi.runAllTimersAsync();
    expect(emittedTypes()).toContain("agent.checkpoint.completed");
    vi.useRealTimers();
  });

  it("uses a child definition's configured timeout for inline execution", async () => {
    vi.useFakeTimers();
    sdk.run.mockImplementation((_agent, _input, options) => new Promise((_resolve, reject) => {
      const signal = (options as { signal: AbortSignal }).signal;
      signal.addEventListener("abort", () => reject(signal.reason), { once: true });
    }));
    const childDefinition = {
      ...definition, kind: "vessica.agent/v2" as const,
      runtime: { kind: "typescript_agents_sdk" as const },
      action_policy: { default: "deny" as const, allowed_actions: [], approval_required: [] },
      writable_knowledge_namespaces: [], sources: { network: "none" as const, allowed_domains: [], allowed_source_types: [] },
      concurrency: 1, timeout_seconds: 2, conversations: { enabled: false, max_turns: 25 }, checkpoints: { enabled: false, interval_seconds: 0 },
    };
    const childExecution = {
      protocol: "vessica.agent-runtime/v1" as const, fence_token: "child_fence",
      task: { id: "child_task", kind: "run" as const, subject_id: "child_run", attempts: 1 },
      run: { id: "child_run", agent_id: "child_agent", input_json: '{"prompt":"child"}', trigger: "child", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
      definition: childDefinition,
    };
    const client = {
      child: vi.fn().mockResolvedValue({ child: { id: "child_run" }, execution: childExecution }),
      events: vi.fn().mockResolvedValue({}), fail: vi.fn().mockResolvedValue({}), complete: vi.fn(),
    } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client, () => ({ stop() {} }));
    const context = { client, runID: "parent_run", fence: "parent_fence", toolOrdinal: 0, batcher: { append: vi.fn().mockResolvedValue(undefined) }, failedToolCallIDs: new Set<string>() };
    const tools = (executor as unknown as { mapTools(d: AgentDefinition, c: typeof context): Array<Record<string, unknown>> }).mapTools({ ...definition, tools: [{ id: "agent.invoke", config: {} }] }, context);
    const invoke = tools[0] as { execute(args: { agent: string; prompt: string }): Promise<unknown> };
    const execution = invoke.execute({ agent: "CHILD", prompt: "run" });
    const rejection = expect(execution).rejects.toThrow("run exceeded 2 second limit");
    await vi.advanceTimersByTimeAsync(2_000);
    await rejection;
    expect(client.fail).toHaveBeenCalledWith("child_run", "child_fence", "run exceeded 2 second limit", expect.any(Object), 0);
  });

  it("serializes two concurrent parents invoking the same v2 concurrency-one child", async () => {
    let finishFirst!: () => void;
    const firstGate = new Promise<void>((resolve) => { finishFirst = resolve; });
    const stream = (gate = Promise.resolve()) => ({
      async *[Symbol.asyncIterator]() { await gate; }, completed: gate, error: null, finalOutput: "done",
      runContext: { usage: { requests: 0, inputTokens: 0, outputTokens: 0, totalTokens: 0, inputTokensDetails: [], outputTokensDetails: [] } },
    });
    sdk.run.mockResolvedValueOnce(stream(firstGate)).mockResolvedValueOnce(stream());
    const childDefinition = {
      ...definition, kind: "vessica.agent/v2" as const, runtime: { kind: "typescript_agents_sdk" as const },
      action_policy: { default: "deny" as const, allowed_actions: [], approval_required: [] }, writable_knowledge_namespaces: [],
      sources: { network: "none" as const, allowed_domains: [], allowed_source_types: [] }, concurrency: 1, timeout_seconds: 30,
      conversations: { enabled: false, max_turns: 25 }, checkpoints: { enabled: false, interval_seconds: 0 },
    };
    const executions = ["child_1", "child_2"].map((id) => ({
      protocol: "vessica.agent-runtime/v1" as const, fence_token: `fence_${id}`,
      task: { id: `task_${id}`, kind: "run" as const, subject_id: id, attempts: 1 },
      run: { id, agent_id: "shared_child", input_json: '{"prompt":"child"}', trigger: "child", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
      definition: childDefinition,
    }));
    const client = {
      child: vi.fn().mockResolvedValueOnce({ child: { id: "child_1" }, execution: executions[0] }).mockResolvedValueOnce({ child: { id: "child_2" }, execution: executions[1] }),
      events: vi.fn().mockResolvedValue({}), complete: vi.fn().mockResolvedValue({}), fail: vi.fn().mockResolvedValue({}),
    } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client, () => ({ stop() {} }), new AgentAdmission());
    const invoke = (runID: string) => {
      const context = { client, runID, fence: `fence_${runID}`, toolOrdinal: 0, batcher: { append: vi.fn().mockResolvedValue(undefined) }, failedToolCallIDs: new Set<string>() };
      return ((executor as unknown as { mapTools(d: AgentDefinition, c: typeof context): Array<Record<string, unknown>> }).mapTools({ ...definition, tools: [{ id: "agent.invoke", config: {} }] }, context)[0] as { execute(args: { agent: string; prompt: string }): Promise<unknown> }).execute({ agent: "CHILD", prompt: "run" });
    };
    const first = invoke("parent_1");
    const second = invoke("parent_2");
    await vi.waitFor(() => expect(sdk.run).toHaveBeenCalledTimes(1));
    finishFirst();
    await Promise.all([first, second]);
    expect(sdk.run).toHaveBeenCalledTimes(2);
  });

  it("keeps v1 inline children compatible without v2 admission", async () => {
    let finish!: () => void;
    const gate = new Promise<void>((resolve) => { finish = resolve; });
    const stream = { async *[Symbol.asyncIterator]() { await gate; }, completed: gate, error: null, finalOutput: "done", runContext: { usage: { requests: 0, inputTokens: 0, outputTokens: 0, totalTokens: 0, inputTokensDetails: [], outputTokensDetails: [] } } };
    sdk.run.mockResolvedValue(stream);
    const child = (id: string) => ({ protocol: "vessica.agent-runtime/v1" as const, fence_token: `fence_${id}`, task: { id: `task_${id}`, kind: "run" as const, subject_id: id, attempts: 1 }, run: { id, agent_id: "legacy", input_json: "{}", trigger: "child", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" }, definition });
    const client = { child: vi.fn().mockResolvedValueOnce({ child: { id: "v1_1" }, execution: child("v1_1") }).mockResolvedValueOnce({ child: { id: "v1_2" }, execution: child("v1_2") }), events: vi.fn().mockResolvedValue({}), complete: vi.fn().mockResolvedValue({}), fail: vi.fn() } as unknown as ControlPlaneClient;
    const executor = new OpenAIAgentsExecutor(client, () => ({ stop() {} }), new AgentAdmission());
    const context = { client, runID: "parent", fence: "parent_fence", toolOrdinal: 0, batcher: { append: vi.fn().mockResolvedValue(undefined) }, failedToolCallIDs: new Set<string>() };
    const invoke = (executor as unknown as { mapTools(d: AgentDefinition, c: typeof context): Array<Record<string, unknown>> }).mapTools({ ...definition, tools: [{ id: "agent.invoke", config: {} }] }, context)[0] as { execute(args: { agent: string; prompt: string }): Promise<unknown> };
    const first = invoke.execute({ agent: "LEGACY", prompt: "one" });
    const second = invoke.execute({ agent: "LEGACY", prompt: "two" });
    await vi.waitFor(() => expect(sdk.run).toHaveBeenCalledTimes(2));
    finish();
    await Promise.all([first, second]);
  });
});
