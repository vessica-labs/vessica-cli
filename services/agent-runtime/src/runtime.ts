import { randomUUID } from "node:crypto";
import type { ControlPlaneClient } from "./control-plane.js";
import { DEFAULT_MODEL, PROTOCOL, type ClaimedTask, type RuntimeCapabilities, type Usage } from "./contracts.js";
import { ExecutionFailure, type Executor } from "./executor.js";
import { intervalLeaseFactory, type LeaseFactory } from "./lease.js";

const emptyUsage = (): Usage => ({ requests: 0, input_tokens: 0, cached_input_tokens: 0, output_tokens: 0, reasoning_tokens: 0, total_tokens: 0, response_ids: [] });
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export class Runtime {
  readonly workerID = `agent-runtime-${randomUUID()}`;
  readonly capabilities: RuntimeCapabilities;
  private active = 0;
  private stopping = false;
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
        "entity.get", "entity.resolve", "entity.create", "epic.list", "epic.view", "epic.create",
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
    const abort = new AbortController();
    const timeout = setTimeout(() => abort.abort(new Error("run exceeded 60 minute limit")), 60 * 60 * 1000);
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
}
