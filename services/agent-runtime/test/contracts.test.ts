import { describe, expect, it } from "vitest";
import { definitionSchema } from "../src/contracts.js";

describe("agent definition", () => {
  it("rejects unsupported reasoning levels", () => {
    const result = definitionSchema.safeParse({ kind: "vessica.agent/v1", name: "A", purpose: "p", system_prompt: "s", model: { id: "gpt-5.6-terra", reasoning_effort: "extreme" }, tools: [], knowledge: [], heartbeat: null, budget: null, eval_critic_agent_id: null });
    expect(result.success).toBe(false);
  });
});
