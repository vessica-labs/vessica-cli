import {
  Agent,
  Runner,
  codeInterpreterTool,
  setTracingDisabled,
  tool,
  webSearchTool,
  type Tool,
} from "@openai/agents";
import { z } from "zod";
import type { ControlPlaneClient } from "./control-plane.js";
import { DEFAULT_MODEL, definitionSchema, parseToolConfig, type AgentDefinition, type ClaimedTask, type Usage } from "./contracts.js";
import { EventBatcher } from "./events.js";
import { intervalLeaseFactory, type LeaseFactory } from "./lease.js";
import { estimateCost, normalizeUsage } from "./usage.js";

const builderDefinitionSchema = definitionSchema.omit({ tools: true }).extend({
  tools: z.array(z.object({ id: z.string(), config_json: z.string() })),
});
const builderOutput = z.object({ definition: builderDefinitionSchema, warnings: z.array(z.string()) });
const criticOutput = z.object({
  score: z.number().min(0).max(1),
  passed: z.boolean(),
  summary: z.string(),
  findings: z.array(z.object({ severity: z.enum(["info", "warning", "error"]), message: z.string() })),
});
type RuntimeContext = {
  client: ControlPlaneClient;
  runID: string;
  fence: string;
  toolOrdinal: number;
  batcher: EventBatcher;
  failedToolCallIDs: Set<string>;
};

const metadataInput = z.array(z.object({ key: z.string(), value: z.string() })).nullable().optional();
const artifactInput = z.object({ artifact_id: z.string().nullable().optional(), type: z.string().nullable().optional(), title: z.string().nullable().optional(), content: z.string().nullable().optional(), status: z.string().nullable().optional(), metadata: metadataInput });
const memoryDetails = {
  title: z.string().nullable().optional(),
  subject: z.string().nullable().optional(),
  predicate: z.string().nullable().optional(),
  object: z.string().nullable().optional(),
  valid_from: z.string().datetime({ offset: true }).nullable().optional(),
  valid_until: z.string().datetime({ offset: true }).nullable().optional(),
  metadata: metadataInput,
};
const memoryCreateInput = z.object({
  type: z.enum(["instruction", "fact", "decision", "episode"]),
  content: z.string().min(1),
  importance: z.number().min(0).max(1),
  confidence: z.number().min(0).max(1),
  confidence_source: z.enum(["human_confirmed", "agent_inferred", "imported", "external_system", "observed"]),
  ...memoryDetails,
});
const memoryVersionInput = z.object({
  memory_id: z.string(),
  type: z.enum(["instruction", "fact", "decision", "episode"]).nullable().optional(),
  content: z.string().min(1).nullable().optional(),
  importance: z.number().min(0).max(1).nullable().optional(),
  confidence: z.number().min(0).max(1).nullable().optional(),
  confidence_source: z.enum(["human_confirmed", "agent_inferred", "imported", "external_system", "observed"]).nullable().optional(),
  ...memoryDetails,
});
export const typedToolSchemas: Record<string, z.ZodObject<any>> = {
  "repository.list": z.object({}),
  "knowledge.retrieve": z.object({ query: z.string(), scopes: z.array(z.string()).default([]), entities: z.array(z.string()).nullable().optional(), artifact_selectors: z.array(z.object({ type: z.string().nullable().optional(), status: z.string().nullable().optional(), id: z.string().nullable().optional(), version: z.number().nullable().optional() })).nullable().optional(), token_budget: z.number().int().positive().default(8000) }),
  "artifact.list": z.object({ type: z.string().nullable().optional(), status: z.string().nullable().optional(), scopes: z.array(z.string()).default([]) }),
  "artifact.get": z.object({ artifact_id: z.string() }),
  "artifact.create": artifactInput,
  "artifact.version": artifactInput.extend({ artifact_id: z.string() }),
  "artifact.activate": z.object({ artifact_id: z.string() }),
  "artifact.supersede": z.object({ artifact_id: z.string() }),
  "memory.list": z.object({ query: z.string().default(""), scopes: z.array(z.string()).default([]) }),
  "memory.search": z.object({ query: z.string(), scopes: z.array(z.string()).default([]) }),
  "memory.get": z.object({ memory_id: z.string() }),
  "memory.create": memoryCreateInput,
  "memory.version": memoryVersionInput,
  "memory.supersede": z.object({ memory_id: z.string() }),
  "memory.archive": z.object({ memory_id: z.string() }),
  "entity.get": z.object({ entity_id: z.string() }),
  "entity.resolve": z.object({ query: z.string(), scopes: z.array(z.string()).default([]) }),
  "entity.create": z.object({ type: z.string(), display_name: z.string(), aliases: z.array(z.string()).default([]), metadata: metadataInput }),
  "epic.list": z.object({}),
  "epic.view": z.object({ epic_id: z.string() }),
  "epic.create": z.object({ repository_id: z.string(), title: z.string(), body: z.string() }),
  "coding_run.start": z.object({ epic_id: z.string(), concurrency: z.number().int().min(1).max(20).default(3), preview: z.boolean().default(true), pr_mode: z.enum(["draft", "ready"]).default("draft") }),
  "coding_run.status": z.object({ run_id: z.string() }),
  "coding_run.events": z.object({ run_id: z.string(), after: z.number().int().min(0).default(0) }),
};

export function normalizeToolArguments(args: unknown): unknown {
  if (!args || typeof args !== "object" || Array.isArray(args)) return args;
  const input = args as Record<string, unknown>;
  if (!Array.isArray(input.metadata)) return args;
  const metadata = Object.fromEntries(input.metadata.map((entry) => {
    const item = entry as { key: string; value: string };
    return [item.key, item.value];
  }));
  return { ...input, metadata };
}

export function consumeFailedToolOutput(failedToolCallIDs: Set<string>, callID?: string): boolean {
  return !!callID && failedToolCallIDs.delete(callID);
}

export class ExecutionFailure extends Error {
  constructor(message: string, readonly usage: Usage, readonly cost: number) {
    super(message);
    this.name = "ExecutionFailure";
  }
}

export interface Executor {
  build(task: ClaimedTask, signal?: AbortSignal): Promise<{ definition: AgentDefinition; warnings: string[]; usage: Usage }>;
  run(task: ClaimedTask, signal: AbortSignal): Promise<{ output: unknown; usage: Usage; cost: number }>;
}

export class OpenAIAgentsExecutor implements Executor {
  private readonly runner: Runner;
  constructor(private readonly client: ControlPlaneClient, private readonly leaseFactory: LeaseFactory = intervalLeaseFactory) {
    setTracingDisabled(true);
    this.runner = new Runner({ tracingDisabled: true, traceIncludeSensitiveData: false, reasoningItemIdPolicy: "preserve" });
  }

  async build(task: ClaimedTask, signal?: AbortSignal) {
    if (!task.build) throw new Error("builder task is incomplete");
    const builder = new Agent({
      name: "Vessica Agent Builder",
      instructions: [
        "Convert the user's request into one safe vessica.agent/v1 definition.",
        "Use only models and tools in the supplied catalogs. Do not invent credentials or channels.",
        "For an update, apply only the requested changes to current_definition and preserve all unspecified fields.",
        "Default model to gpt-5.6-terra with medium reasoning and budget to $5.00/day in the supplied client timezone (or UTC if absent).",
        "Each tool uses config_json containing a JSON object string. Use {} unless the request needs a supported tool option.",
        "Return unsupported requests as concise warnings. Names contain no whitespace.",
      ].join(" "),
      model: DEFAULT_MODEL,
      modelSettings: { reasoning: { effort: "medium" } },
      outputType: builderOutput,
    });
    const input = JSON.stringify({
      operation: task.build.kind,
      description: task.build.description,
      current_definition: task.current_definition,
      client_timezone: task.client_timezone || "UTC",
      models: task.model_catalog,
      tools: task.tool_catalog,
      agents: task.agent_catalog,
    });
    const result = await this.runner.run(builder, input, { stream: true, maxTurns: 3, signal });
    for await (const _event of result) {
      // Consuming the SDK stream keeps long builder requests cancellable and lease-heartbeating.
    }
    await result.completed;
    if (result.error) throw result.error;
    if (!result.finalOutput) throw new Error("builder returned no structured definition");
    const definition: AgentDefinition = {
      ...result.finalOutput.definition,
      tools: result.finalOutput.definition.tools.map(({ id, config_json }) => {
        let config: unknown;
        try { config = JSON.parse(config_json); }
        catch { throw new Error(`builder returned invalid config_json for ${id}`); }
        return { id, config: parseToolConfig(id, config) };
      }),
    };
    return {
      definition,
      warnings: result.finalOutput.warnings,
      usage: normalizeUsage(result.runContext.usage, result.rawResponses.map((r) => r.responseId).filter((id): id is string => !!id)),
    };
  }

  async run(task: ClaimedTask, signal: AbortSignal) {
    if (!task.run || !task.definition) throw new Error("run task is incomplete");
    const definition = task.definition;
    const batcher = new EventBatcher(this.client, task.run.id, task.fence_token);
    const context: RuntimeContext = {
      client: this.client,
      runID: task.run.id,
      fence: task.fence_token,
      toolOrdinal: 0,
      batcher,
      failedToolCallIDs: new Set(),
    };
    const tools = this.mapTools(definition, context);
    const registry = (task.agent_registry ?? []).map((a) => `${a.id} ${a.name}: ${a.purpose}`).join("\n");
    const repositories = (task.repositories ?? []).map((repository) => `${repository.id}: ${repository.display_name ?? repository.canonical_remote ?? "repository"}`).join("\n");
    const exactKnowledge = JSON.parse(task.run.resolved_knowledge_json || "[]") as Array<{artifact_id:string;version_id:string;description:string}>;
    const knowledge = exactKnowledge.map((k) => `${k.artifact_id} (${k.version_id}): ${k.description}`).join("\n");
    const instructions = [
      definition.system_prompt,
      knowledge ? `\nKnowledge references (retrieve full content only with an enabled tool):\n${knowledge}` : "",
      registry ? `\nActive agent registry:\n${registry}` : "",
      repositories ? `\nAvailable repositories:\n${repositories}` : "",
      task.run.originating_repository_id ? `\nOriginating repository: ${task.run.originating_repository_id}` : "",
    ].join("\n");
    const agent = new Agent<RuntimeContext, typeof criticOutput | "text">({
      name: definition.name,
      instructions,
      model: definition.model.id,
      modelSettings: { reasoning: { effort: definition.model.reasoning_effort } },
      tools,
      outputType: task.task.kind === "eval" ? criticOutput : "text",
    });
    await batcher.append("agent.run.started", { attempt: task.attempt?.attempt_number ?? 1, trigger: task.run.trigger });
    const parsed = JSON.parse(task.run.input_json) as { prompt?: string };
    const modelInput = task.task.kind === "eval" ? task.run.input_json : parsed.prompt ?? task.run.input_json;
    const stream = await this.runner.run(agent, modelInput, { stream: true, maxTurns: 25, context, signal });
    const responseIDs: string[] = [];
    let completedText = "";
    try {
      for await (const event of stream) {
        if (event.type === "raw_model_stream_event") {
          const data = event.data as unknown as { type?: string; delta?: string; response?: { id?: string } };
          if (data.type === "output_text_delta" && data.delta) {
            completedText += data.delta;
            await batcher.append("agent.message.delta", { text: data.delta });
          }
          if (data.type === "response_done") {
            if (data.response?.id) responseIDs.push(data.response.id);
            const usage = normalizeUsage(stream.runContext.usage, responseIDs);
            await this.client.usage(task.run.id, task.fence_token, usage);
            await batcher.append("agent.usage", usage);
          }
        } else if (event.type === "run_item_stream_event") {
          const item = event.item.toJSON().rawItem as { name?: string; callId?: string; status?: string };
          if (event.name === "tool_called") await batcher.append("agent.tool.started", { tool: item.name, call_id: item.callId });
          if (event.name === "tool_output") {
            if (consumeFailedToolOutput(context.failedToolCallIDs, item.callId)) continue;
            await batcher.append(item.status === "failed" ? "agent.tool.failed" : "agent.tool.completed", { tool: item.name, call_id: item.callId, status: item.status });
          }
        }
      }
      await stream.completed;
      if (stream.error) throw stream.error;
    } catch (error) {
      const usage = normalizeUsage(stream.runContext.usage, responseIDs);
      await batcher.append("error", { message: error instanceof Error ? error.message : "agent run failed" });
      await batcher.flush().catch(() => undefined);
      throw new ExecutionFailure(error instanceof Error ? error.message : "agent run failed", usage, estimateCost(usage, task.run.rate_snapshot_json));
    }
    const usage = normalizeUsage(stream.runContext.usage, responseIDs);
    const finalText = typeof stream.finalOutput === "string" ? stream.finalOutput : completedText;
    await batcher.append("agent.message.completed", { text: finalText });
    if (task.task.kind === "eval") await batcher.append("agent.eval.completed", stream.finalOutput);
    await batcher.append("agent.run.completed", { response_ids: responseIDs });
    await batcher.flush();
    return { output: stream.finalOutput ?? completedText, usage, cost: estimateCost(usage, task.run.rate_snapshot_json) };
  }

  private mapTools(definition: AgentDefinition, context: RuntimeContext): Tool<RuntimeContext>[] {
    return (definition.tools ?? []).map(({ id, config }) => {
      const parsed = parseToolConfig(id, config ?? {});
      if (id === "openai.web_search") {
        const web = parsed as {
          search_context_size?: "low" | "medium" | "high";
          allowed_domains?: string[];
          external_web_access?: boolean;
          user_location?: { type?: "approximate"; city?: string | null; country?: string | null; region?: string | null; timezone?: string | null };
        };
        return webSearchTool({
          searchContextSize: web.search_context_size,
          filters: web.allowed_domains ? { allowedDomains: web.allowed_domains } : undefined,
          externalWebAccess: web.external_web_access,
          userLocation: web.user_location,
        }) as Tool<RuntimeContext>;
      }
      if (id === "openai.code_interpreter") {
        const code = parsed as { include_outputs?: boolean; container?: unknown };
        return codeInterpreterTool({ includeOutputs: code.include_outputs, container: code.container } as Parameters<typeof codeInterpreterTool>[0]) as Tool<RuntimeContext>;
      }
      if (id === "agent.invoke") return tool({
        name: "agent_invoke", description: "Invoke another registered Vessica agent as a durable child run.",
        parameters: z.object({ agent: z.string(), prompt: z.string() }),
        execute: async ({ agent, prompt }) => {
          const child = await context.client.child(context.runID, context.fence, agent, prompt);
          await context.batcher.append("agent.child.started", { run_id: child.child.id, agent });
          if (!child.execution) return child.child;
          try {
            const result = await this.runInlineChild(child.execution);
            await context.batcher.append("agent.child.completed", { run_id: child.child.id, status: "completed" });
            return { run_id: child.child.id, status: "completed", output: result.output };
          } catch (error) {
            await context.batcher.append("agent.child.completed", { run_id: child.child.id, status: "failed" });
            throw error;
          }
        },
      });
      const parameters = typedToolSchemas[id];
      if (!parameters) throw new Error(`runtime has no typed schema for tool ${id}`);
      return tool({
        name: id.replaceAll(".", "_"), description: `Vessica typed tool ${id}.`,
        parameters,
        execute: async (args, _runContext, details) => this.invokeControlPlaneTool(context, id, normalizeToolArguments(args), details?.toolCall?.callId),
      });
    });
  }

  private async invokeControlPlaneTool(context: RuntimeContext, id: string, args: unknown, callID?: string) {
    try {
      return (await context.client.tool(context.runID, context.fence, id, ++context.toolOrdinal, args)).result;
    } catch (error) {
      if (callID) context.failedToolCallIDs.add(callID);
      await context.batcher.append("agent.tool.failed", { tool: id, call_id: callID, message: error instanceof Error ? error.message : "tool failed" });
      throw error;
    }
  }

  private async runInlineChild(task: ClaimedTask) {
    const abort = new AbortController();
    const timeout = setTimeout(() => abort.abort(new Error("run exceeded 60 minute limit")), 60 * 60 * 1000);
    const lease = this.leaseFactory(this.client, task, (reason) => abort.abort(reason));
    try {
      const result = await this.run(task, abort.signal);
      await this.client.complete(task.task.subject_id, task.fence_token, result.output, result.usage, result.cost);
      return result;
    } catch (error) {
      const usage = error instanceof ExecutionFailure ? error.usage : { requests:0,input_tokens:0,cached_input_tokens:0,output_tokens:0,reasoning_tokens:0,total_tokens:0,response_ids:[] };
      const cost = error instanceof ExecutionFailure ? error.cost : 0;
      await this.client.fail(task.task.subject_id, task.fence_token, error instanceof Error ? error.message : "child run failed", usage, cost).catch(()=>undefined);
      throw error;
    } finally { clearTimeout(timeout); lease.stop(); }
  }
}
