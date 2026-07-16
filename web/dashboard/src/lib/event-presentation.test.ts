import { describe, expect, it } from "vitest";
import { presentEvent } from "./event-presentation";

describe("presentEvent", () => {
  it("preserves agent messages for Markdown chat rendering", () => {
    expect(
      presentEvent({ type: "agent.message", payload: { role: "coder", message: "## Done\n\n- shipped it" } }),
    ).toMatchObject({ agentMessage: true, title: "Coder message", message: "## Done\n\n- shipped it" });
  });

  it("summarizes operational events while retaining inspectable details", () => {
    expect(
      presentEvent({ type: "validation.step", payload: { phase: "validate", step: "Landing page loads" } }),
    ).toMatchObject({ agentMessage: false, title: "Validation Step", summary: "Landing page loads" });
  });
});
