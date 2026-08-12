import { z } from "zod";

export const PROTOCOL = "vessica.agent-runtime/v1";
export const DEFAULT_MODEL = "gpt-5.6-terra";

const emptyToolConfigSchema = z.object({}).strict();
export const webSearchConfigSchema = z.object({
  search_context_size: z.enum(["low", "medium", "high"]).optional(),
  allowed_domains: z.array(z.string().min(1)).optional(),
  external_web_access: z.boolean().optional(),
  user_location: z.object({
    type: z.literal("approximate").optional(),
    city: z.string().nullable().optional(),
    country: z.string().length(2).nullable().optional(),
    region: z.string().nullable().optional(),
    timezone: z.string().nullable().optional(),
  }).strict().optional(),
}).strict();
export const codeInterpreterConfigSchema = z.object({
  include_outputs: z.boolean().optional(),
  container: z.union([
    z.string().min(1),
    z.object({
      type: z.literal("auto"),
      file_ids: z.array(z.string().min(1)).optional(),
      memory_limit: z.enum(["1g", "4g", "16g", "64g"]).nullable().optional(),
    }).strict(),
  ]).optional(),
}).strict();

export function parseToolConfig(toolID: string, value: unknown): Record<string, unknown> {
  if (toolID === "openai.web_search") return webSearchConfigSchema.parse(value);
  if (toolID === "openai.code_interpreter") return codeInterpreterConfigSchema.parse(value);
  return emptyToolConfigSchema.parse(value);
}

const definitionCore = {
  name: z.string().min(1).max(64),
  purpose: z.string().min(1).max(2000),
  system_prompt: z.string().min(1).max(65536),
  model: z.object({
    id: z.string(),
    reasoning_effort: z.enum(["low", "medium", "high", "xhigh"]),
  }),
  tools: z.array(z.object({ id: z.string(), config: z.record(z.string(), z.unknown()).default({}) })).default([]),
  knowledge: z.array(z.object({
    artifact_id: z.string(), description: z.string(), version: z.string(),
  })).default([]),
  heartbeat: z.object({
    enabled: z.boolean(), cron: z.string(), timezone: z.string(),
  }).nullable().default(null),
  budget: z.object({ daily_usd: z.string(), timezone: z.string() }).nullable().default(null),
  eval_critic_agent_id: z.string().nullable().default(null),
};

export const definitionV1Schema = z.object({
  kind: z.literal("vessica.agent/v1"),
  ...definitionCore,
});

export const definitionV2Schema = z.object({
  kind: z.literal("vessica.agent/v2"),
  ...definitionCore,
  runtime: z.object({ kind: z.literal("typescript_agents_sdk") }).default({ kind: "typescript_agents_sdk" }),
  action_policy: z.object({
    default: z.enum(["allow", "deny"]),
    allowed_actions: z.array(z.string()).default([]),
    approval_required: z.array(z.string()).default([]),
  }),
  writable_knowledge_namespaces: z.array(z.string()).default([]),
  sources: z.object({
    network: z.enum(["none", "allowlist", "public"]),
    allowed_domains: z.array(z.string()).default([]),
    allowed_source_types: z.array(z.enum(["rss", "atom", "web", "reddit", "x"])).default([]),
  }).default({ network: "none", allowed_domains: [], allowed_source_types: [] }),
  concurrency: z.number().int().min(1).max(20).default(1),
  timeout_seconds: z.number().int().min(1).max(86400).default(3600),
  conversations: z.object({ enabled: z.boolean(), max_turns: z.number().int().min(1).max(200) }).default({ enabled: false, max_turns: 25 }),
  checkpoints: z.object({ enabled: z.boolean(), interval_seconds: z.number().int().min(0).max(3600) }).default({ enabled: true, interval_seconds: 30 }),
});

export const definitionSchema = z.union([definitionV1Schema, definitionV2Schema]);

export type AgentDefinition = z.infer<typeof definitionSchema>;
export type NormalizedAgentDefinition = AgentDefinition & {
  runtime: { kind: "typescript_agents_sdk" };
  concurrency: number;
  timeout_seconds: number;
  conversations: { enabled: boolean; max_turns: number };
  checkpoints: { enabled: boolean; interval_seconds: number };
};

export function normalizeDefinition(value: unknown): NormalizedAgentDefinition {
  const parsed = definitionSchema.parse(value);
  if (parsed.kind === "vessica.agent/v2") return parsed;
  return {
    ...parsed,
    runtime: { kind: "typescript_agents_sdk" },
    concurrency: 1,
    timeout_seconds: 3600,
    conversations: { enabled: false, max_turns: 25 },
    checkpoints: { enabled: true, interval_seconds: 30 },
  };
}

export type RuntimeCapabilities = {
  runtime_version: string;
  protocol: typeof PROTOCOL;
  sdk_version: string;
  models: string[];
  tools: string[];
  concurrency: number;
  credentials_ready: boolean;
};

export type ClaimedTask = {
  protocol: typeof PROTOCOL;
  fence_token: string;
  task: { id: string; kind: "build" | "run" | "eval"; subject_id: string; attempts: number };
  attempt?: { id: string; attempt_number: number };
  build?: { id: string; kind: "create" | "update"; description: string; agent_id?: string };
  client_timezone?: string;
  run?: { id: string; agent_id?: string; input_json: string; trigger: string; originating_repository_id?: string; rate_snapshot_json: string; resolved_knowledge_json: string };
  definition?: AgentDefinition;
  current_definition?: AgentDefinition;
  agent_catalog?: Array<{ id: string; name: string; purpose: string }>;
  agent_registry?: Array<{ id: string; name: string; purpose: string }>;
  repositories?: Array<{ id: string; display_name?: string; canonical_remote?: string }>;
  model_catalog?: string[];
  tool_catalog?: string[];
};

export type NormalizedEvent = { ordinal: number; type: string; payload: unknown };

export type Usage = {
  requests: number;
  input_tokens: number;
  cached_input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  total_tokens: number;
  response_ids: string[];
};
