import { tool } from "@openai/agents";
import { describe, expect, it } from "vitest";

import { normalizeToolArguments, typedToolSchemas } from "../src/executor.js";

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
});
