import { describe, expect, it } from "vitest";
import { estimateCost, normalizeUsage } from "../src/usage.js";

describe("usage accounting", () => {
  it("keeps cached and reasoning tokens separate", () => {
    const usage = normalizeUsage({ requests: 1, inputTokens: 100, outputTokens: 20, totalTokens: 120, inputTokensDetails: [{ cached_tokens: 40 }], outputTokensDetails: [{ reasoning_tokens: 5 }] }, ["resp_1"]);
    expect(usage.cached_input_tokens).toBe(40);
    expect(usage.reasoning_tokens).toBe(5);
    expect(estimateCost(usage, JSON.stringify({ input_microusd_per_million: 2_000_000, cached_input_microusd_per_million: 200_000, output_microusd_per_million: 10_000_000 }))).toBe(328);
  });
});
