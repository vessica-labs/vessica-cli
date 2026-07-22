import { tool } from "@openai/agents";
import { describe, expect, it } from "vitest";

import { typedToolSchemas } from "../src/executor.js";

describe("typed Vessica tool schemas", () => {
  it("converts every schema through the real Agents SDK strict JSON Schema path", () => {
    for (const [id, parameters] of Object.entries(typedToolSchemas)) {
      expect(() => tool({
        name: id.replaceAll(".", "_"),
        description: `Vessica typed tool ${id}.`,
        parameters,
        execute: async () => ({}),
      }), id).not.toThrow();
    }
  });
});
