import { Worker } from "node:worker_threads";
import type { ControlPlaneClient } from "./control-plane.js";
import type { ClaimedTask } from "./contracts.js";

export type LeaseHandle = { stop(): void };
export type LeaseFactory = (client: ControlPlaneClient, task: ClaimedTask, onAbort: (reason: Error) => void) => LeaseHandle;

export const intervalLeaseFactory: LeaseFactory = (client, task, onAbort) => {
  let stopped = false;
  let running = false;
  let failures = 0;
  const beat = async () => {
    if (stopped || running) return;
    running = true;
    try {
      const result = await client.heartbeat(task.task.id, task.task.subject_id, task.fence_token);
      failures = 0;
      if (result.cancel_requested) onAbort(new Error("run cancelled"));
    } catch {
      failures++;
      if (failures >= 3) onAbort(new Error("attempt lease lost"));
    } finally {
      running = false;
    }
  };
  const timer = setInterval(() => { void beat(); }, 15_000);
  return { stop: () => { stopped = true; clearInterval(timer); } };
};

export const workerLeaseFactory: LeaseFactory = (client, task, onAbort) => {
  const connection = client.runtimeConnection();
  let stopped = false;
  const worker = new Worker(new URL("./lease-worker.js", import.meta.url), {
    workerData: {
      ...connection,
      task_id: task.task.id,
      subject_id: task.task.subject_id,
      fence_token: task.fence_token,
    },
  });
  worker.on("message", (message: { type?: string }) => {
    if (message.type === "cancelled") onAbort(new Error("run cancelled"));
    if (message.type === "lost") onAbort(new Error("attempt lease lost"));
  });
  worker.on("error", () => {
    if (!stopped) onAbort(new Error("attempt lease lost"));
  });
  worker.on("exit", (code) => {
    if (!stopped && code !== 0) onAbort(new Error("attempt lease lost"));
  });
  return {
    stop: () => {
      stopped = true;
      worker.postMessage({ type: "stop" });
      void worker.terminate();
    },
  };
};
