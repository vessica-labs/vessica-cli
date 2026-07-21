import { describe, expect, it, vi } from "vitest";
import type { ControlPlaneClient } from "../src/control-plane.js";
import { EventBatcher } from "../src/events.js";

describe("EventBatcher", () => {
  it("persists streamed text as resumable chunks instead of token rows", async () => {
    const client = { events: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const batcher = new EventBatcher(client, "arun_1", "fence_1");
    await batcher.append("agent.message.delta", { text: "hel" });
    await batcher.append("agent.message.delta", { text: "lo" });
    await batcher.flush();
    expect(client.events).toHaveBeenCalledWith("arun_1", "fence_1", [
      { ordinal: 1, type: "agent.message.delta", payload: { text: "hello" } },
    ]);
  });
});
