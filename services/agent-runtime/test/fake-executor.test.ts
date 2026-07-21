import { describe, expect, it, vi } from "vitest";
import { FakeExecutor } from "../src/fake-executor.js";
import type { ClaimedTask } from "../src/contracts.js";
import type { ControlPlaneClient } from "../src/control-plane.js";

describe("FakeExecutor", () => {
  it("emits a deterministic critic result through the real runtime protocol", async () => {
    const client = { events: vi.fn().mockResolvedValue({}), usage: vi.fn().mockResolvedValue({}) } as unknown as ControlPlaneClient;
    const task = {
      protocol: "vessica.agent-runtime/v1",
      fence_token: "fence_1",
      task: { id: "atask_1", kind: "eval", subject_id: "arun_eval", attempts: 1 },
      attempt: { id: "attempt_1", attempt_number: 1 },
      run: { id: "arun_eval", input_json: "{}", trigger: "eval", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
    } satisfies ClaimedTask;
    const result = await new FakeExecutor(client).run(task, new AbortController().signal);
    expect(result.output).toMatchObject({ score: 0.97, passed: true });
    expect(client.events).toHaveBeenCalled();
    expect(client.usage).toHaveBeenCalledWith("arun_eval", "fence_1", result.usage);
  });
});
