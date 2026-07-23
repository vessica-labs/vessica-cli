import { beforeEach, describe, expect, it, vi } from "vitest";

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
});
