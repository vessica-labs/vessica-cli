import { describe, expect, it } from "vitest";
import { AgentAdmission } from "../src/admission.js";
import type { ClaimedTask } from "../src/contracts.js";

const task = (runID: string, agentID: string): ClaimedTask => ({
  protocol: "vessica.agent-runtime/v1", fence_token: `fence_${runID}`,
  task: { id: `task_${runID}`, kind: "run", subject_id: runID, attempts: 1 },
  run: { id: runID, agent_id: agentID, input_json: "{}", trigger: "child", rate_snapshot_json: "{}", resolved_knowledge_json: "[]" },
  definition: {
    kind: "vessica.agent/v2", name: agentID, purpose: "test", system_prompt: "test",
    model: { id: "gpt-5.6-terra", reasoning_effort: "medium" }, tools: [], knowledge: [], heartbeat: null, budget: null, eval_critic_agent_id: null,
    runtime: { kind: "typescript_agents_sdk" }, action_policy: { default: "deny", allowed_actions: [], approval_required: [] },
    writable_knowledge_namespaces: [], sources: { network: "none", allowed_domains: [], allowed_source_types: [] },
    concurrency: 1, timeout_seconds: 30, conversations: { enabled: false, max_turns: 25 }, checkpoints: { enabled: false, interval_seconds: 0 },
  },
});

describe("AgentAdmission", () => {
  it("rejects one side of a nested wait cycle instead of deadlocking", async () => {
    const admission = new AgentAdmission();
    const releaseA = await admission.admit(task("parent_a", "agent_a"));
    const releaseB = await admission.admit(task("parent_b", "agent_b"));
    const childB = admission.admit(task("child_b", "agent_b"), "parent_a");
    await Promise.resolve();
    await expect(admission.admit(task("child_a", "agent_a"), "parent_b")).rejects.toThrow("deadlock avoided");
    releaseB();
    const releaseChildB = await childB;
    releaseChildB();
    releaseA();
  });
});
