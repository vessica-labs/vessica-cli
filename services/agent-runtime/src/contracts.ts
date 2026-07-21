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

export const definitionSchema = z.object({
  kind: z.literal("vessica.agent/v1"),
  name: z.string().min(1).max(64),
  purpose: z.string().min(1).max(2000),
  system_prompt: z.string().min(1).max(65536),
  model: z.object({
    id: z.string(),
    reasoning_effort: z.enum(["low", "medium", "high", "xhigh"]),
  }),
  tools: z.array(z.object({ id: z.string(), config: z.record(z.string(), z.unknown()).default({}) })),
  knowledge: z.array(z.object({
    artifact_id: z.string(), description: z.string(), version: z.string(),
  })),
  heartbeat: z.object({
    enabled: z.boolean(), cron: z.string(), timezone: z.string(),
  }).nullable(),
  budget: z.object({ daily_usd: z.string(), timezone: z.string() }).nullable(),
  eval_critic_agent_id: z.string().nullable(),
});

export type AgentDefinition = z.infer<typeof definitionSchema>;

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
  run?: { id: string; input_json: string; trigger: string; originating_repository_id?: string; rate_snapshot_json: string; resolved_knowledge_json: string };
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
