import type { ClaimedTask } from "./contracts.js";

type Release = () => void;

// AgentAdmission is shared by top-level and inline execution. It enforces v2
// limits per durable agent ID and tracks nested wait dependencies so opposing
// parent/child calls fail one side instead of deadlocking the runtime.
export class AgentAdmission {
  private readonly active = new Map<string, Set<string>>();
  private readonly waiters = new Map<string, Set<() => void>>();
  private readonly waitsFor = new Map<string, Set<string>>();

  async admit(task: ClaimedTask, requesterRunID?: string, signal?: AbortSignal): Promise<Release> {
    if (task.definition?.kind !== "vessica.agent/v2" || !task.run) return () => undefined;
    const agentID = task.run.agent_id || task.definition.name;
    const holderID = task.run.id;
    const limit = task.definition.concurrency;
    while ((this.active.get(agentID)?.size ?? 0) >= limit) {
      const blockers = new Set(this.active.get(agentID) ?? []);
      if (requesterRunID) {
        this.waitsFor.set(requesterRunID, blockers);
        if ([...blockers].some((blocker) => blocker === requesterRunID || this.hasPath(blocker, requesterRunID))) {
          this.waitsFor.delete(requesterRunID);
          throw new Error(`agent ${agentID} concurrency admission deadlock avoided`);
        }
      }
      try {
        await this.wait(agentID, signal);
      } finally {
        if (requesterRunID) this.waitsFor.delete(requesterRunID);
      }
    }
    const holders = this.active.get(agentID) ?? new Set<string>();
    holders.add(holderID);
    this.active.set(agentID, holders);
    return () => {
      const current = this.active.get(agentID);
      current?.delete(holderID);
      if (!current?.size) this.active.delete(agentID);
      const waiting = this.waiters.get(agentID) ?? new Set<() => void>();
      this.waiters.delete(agentID);
      for (const resume of waiting) resume();
    };
  }

  private hasPath(from: string, target: string, seen = new Set<string>()): boolean {
    if (from === target) return true;
    if (seen.has(from)) return false;
    seen.add(from);
    return [...(this.waitsFor.get(from) ?? [])].some((next) => this.hasPath(next, target, seen));
  }

  private wait(agentID: string, signal?: AbortSignal): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        signal?.removeEventListener("abort", abort);
        const waiting = this.waiters.get(agentID);
        waiting?.delete(resume);
        if (!waiting?.size) this.waiters.delete(agentID);
      };
      const resume = () => { cleanup(); resolve(); };
      const abort = () => { cleanup(); reject(signal?.reason ?? new Error("admission cancelled")); };
      const waiting = this.waiters.get(agentID) ?? new Set<() => void>();
      waiting.add(resume);
      this.waiters.set(agentID, waiting);
      if (signal?.aborted) abort();
      else signal?.addEventListener("abort", abort, { once: true });
    });
  }
}
