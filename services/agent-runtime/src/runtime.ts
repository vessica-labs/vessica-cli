import { randomUUID } from "node:crypto";
import type { ControlPlaneClient } from "./control-plane.js";
import { DEFAULT_MODEL, PROTOCOL, type ClaimedTask, type RuntimeCapabilities, type Usage } from "./contracts.js";
import { ExecutionFailure, type Executor } from "./executor.js";
import { intervalLeaseFactory, type LeaseFactory } from "./lease.js";

const emptyUsage = (): Usage => ({ requests: 0, input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, total_tokens: 0, response_ids: [] });
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

// Runtime lifecycle is intentionally provider-neutral. Future runtimes can
// implement this boundary without moving model execution into the Go control
// plane. Codex Sandbox is deliberately not implemented.
export interface AgentRuntimeLifecycle {
  start(): Promise<void>;
  stop(): void;
  execute(task: ClaimedTask): Promise<void>;
}

export class Runtime implements AgentRuntimeLifecycle {
  readonly workerID = `agent-runtime-${randomUUID()}`;
  readonly capabilities: RuntimeCapabilities;
  private active = 0;
  private stopping = false;
  private readonly agentActive = new Map<string, number>();
  private readonly agentWaiters = new Map<string, Array<() => void>>();
  constructor(private readonly client: ControlPlaneClient, private readonly executor: Executor, concurrency = 4, credentialsReady = !!process.env.OPENAI_API_KEY, private readonly leaseFactory: LeaseFactory = intervalLeaseFactory) {
    this.capabilities = {
      runtime_version: process.env.RUNTIME_VERSION || "dev",
      protocol: PROTOCOL,
      sdk_version: "0.13.5",
      models: [DEFAULT_MODEL],
      tools: [
        "openai.web_search", "openai.code_interpreter", "repository.list", "knowledge.retrieve",
        "artifact.list", "artifact.get", "artifact.create", "artifact.version", "artifact.activate", "artifact.supersede",
        "memory.list", "memory.get", "memory.search", "memory.create", "memory.version", "memory.supersede", "memory.archive",
        "entity.get", "entity.resolve", "entity.create", "entity.merge", "epic.list", "epic.view", "epic.create",
        "coding_run.start", "coding_run.status", "coding_run.events", "agent.invoke",
      ],
      concurrency,
      credentials_ready: credentialsReady,
    };
  }
  stop() { this.stopping = true; }
  async start() {
    if (!this.capabilities.credentials_ready) {
      while (!this.stopping) {
        await this.client.capabilities(this.capabilities).catch(() => undefined);
        await sleep(30_000);
      }
      return;
    }
    while (!this.stopping) {
      try {
        await this.client.capabilities(this.capabilities);
        break;
      } catch {
        await sleep(1_000);
      }
    }
    while (!this.stopping) {
      if (this.active >= this.capabilities.concurrency) { await sleep(100); continue; }
      let task: ClaimedTask | undefined;
      try {
        task = await this.client.claim(this.workerID, this.capabilities);
      } catch {
        await sleep(1_000);
        continue;
      }
      if (!task) { await sleep(750); continue; }
      this.active++;
      void this.execute(task).finally(() => this.active--);
    }
  }
  async execute(task: ClaimedTask) {
    const release = await this.admit(task);
    try {
      await this.executeClaimed(task);
    } finally {
      release();
    }
  }
  private async executeClaimed(task: ClaimedTask) {
    const abort = new AbortController();
    const timeoutSeconds = task.definition?.kind === "vessica.agent/v2" ? task.definition.timeout_seconds ?? 60 * 60 : 60 * 60;
    const timeout = setTimeout(() => abort.abort(new Error(`run exceeded ${timeoutSeconds} second limit`)), timeoutSeconds * 1000);
    const lease = this.leaseFactory(this.client, task, (reason) => abort.abort(reason));
    let usage = emptyUsage();
    try {
      if (task.task.kind === "build") {
        const result = await this.executor.build(task, abort.signal);
        await this.client.completeBuild(task.task.subject_id, task.fence_token, result.definition, result.warnings, result.usage);
        return;
      }
      const result = await this.executor.run(task, abort.signal);
      usage = result.usage;
      await this.client.complete(task.task.subject_id, task.fence_token, result.output, usage, result.cost);
    } catch (error) {
      const message = error instanceof Error ? error.message : "agent task failed";
      if (task.task.kind === "build") await this.client.failTask(task.task.id, task.fence_token, message).catch(() => undefined);
      else {
        if (error instanceof ExecutionFailure) usage = error.usage;
        await this.client.fail(task.task.subject_id, task.fence_token, message, usage, error instanceof ExecutionFailure ? error.cost : 0).catch(() => undefined);
      }
    } finally {
      clearTimeout(timeout);
      lease.stop();
    }
  }

  private async admit(task: ClaimedTask): Promise<() => void> {
    if (task.definition?.kind !== "vessica.agent/v2" || !task.run) return () => undefined;
    const key = task.run.agent_id || task.definition.name;
    const limit = task.definition.concurrency;
    while ((this.agentActive.get(key) ?? 0) >= limit) {
      await new Promise<void>((resolve) => {
        const waiters = this.agentWaiters.get(key) ?? [];
        waiters.push(resolve);
        this.agentWaiters.set(key, waiters);
      });
    }
    this.agentActive.set(key, (this.agentActive.get(key) ?? 0) + 1);
    return () => {
      const remaining = (this.agentActive.get(key) ?? 1) - 1;
      if (remaining > 0) this.agentActive.set(key, remaining);
      else this.agentActive.delete(key);
      const waiters = this.agentWaiters.get(key) ?? [];
      this.agentWaiters.delete(key);
      for (const resume of waiters) resume();
    };
  }
}
