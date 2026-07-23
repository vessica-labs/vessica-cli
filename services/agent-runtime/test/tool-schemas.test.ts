import { tool } from "@openai/agents";
import { describe, expect, it } from "vitest";

import { consumeFailedToolOutput, normalizeToolArguments, typedToolSchemas } from "../src/executor.js";

describe("typed Vessica tool schemas", () => {
  it("converts every schema through the real Agents SDK strict JSON Schema path", () => {
    for (const [id, parameters] of Object.entries(typedToolSchemas)) {
      const converted = tool({
        name: id.replaceAll(".", "_"),
        description: `Vessica typed tool ${id}.`,
        parameters,
        execute: async () => ({}),
      });
      expect(JSON.stringify(converted), id).not.toContain("propertyNames");
    }
  });

  it("normalizes API-safe metadata entries for control-plane tools", () => {
    expect(normalizeToolArguments({
      title: "Client follow-up",
      metadata: [
        { key: "source", value: "email" },
        { key: "confidence", value: "0.9" },
      ],
    })).toEqual({
      title: "Client follow-up",
      metadata: { source: "email", confidence: "0.9" },
    });
  });

  it("enforces the hosted knowledge server memory contract", () => {
    const valid = typedToolSchemas["memory.create"]?.parse({
      type: "decision",
      title: "Big Rock ranking",
      content: "Market stature is the first priority.",
      importance: 1,
      confidence: 1,
      confidence_source: "human_confirmed",
    });
    expect(valid).toMatchObject({ type: "decision", confidence_source: "human_confirmed" });
    expect(valid).not.toHaveProperty("scope_id");
    expect(() => typedToolSchemas["memory.create"]?.parse({
      type: "guess",
      content: "invalid",
      importance: 2,
      confidence: -1,
      confidence_source: "maybe",
    })).toThrow();
  });

  it("suppresses the SDK success-shaped output after a failed tool call", () => {
    const failed = new Set(["call_failed"]);
    expect(consumeFailedToolOutput(failed, "call_failed")).toBe(true);
    expect(consumeFailedToolOutput(failed, "call_failed")).toBe(false);
    expect(consumeFailedToolOutput(failed, "call_ok")).toBe(false);
  });
});
