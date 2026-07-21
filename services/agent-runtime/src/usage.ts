import type { Usage } from "./contracts.js";

type SDKUsage = {
  requests: number; inputTokens: number; outputTokens: number; totalTokens: number;
  inputTokensDetails: Array<Record<string, number>>;
  outputTokensDetails: Array<Record<string, number>>;
};
export function normalizeUsage(value: SDKUsage, responseIDs: string[]): Usage {
  const sum = (items: Array<Record<string, number>>, keys: string[]) => items.reduce((total, item) => total + keys.reduce((n, key) => n + (item[key] ?? 0), 0), 0);
  return {
    requests: value.requests,
    input_tokens: value.inputTokens,
    cached_input_tokens: sum(value.inputTokensDetails, ["cached_tokens", "cachedTokens"]),
    output_tokens: value.outputTokens,
    reasoning_tokens: sum(value.outputTokensDetails, ["reasoning_tokens", "reasoningTokens"]),
    total_tokens: value.totalTokens,
    response_ids: responseIDs,
  };
}
export function estimateCost(usage: Usage, snapshotJSON: string): number {
  const rates = JSON.parse(snapshotJSON || "{}") as Record<string, number>;
  const uncached = Math.max(0, usage.input_tokens - usage.cached_input_tokens);
  const numerator = uncached * (rates.input_microusd_per_million ?? 0)
    + usage.cached_input_tokens * (rates.cached_input_microusd_per_million ?? 0)
    + usage.output_tokens * (rates.output_microusd_per_million ?? 0);
  return Math.max(0, Math.round(numerator / 1_000_000));
}
