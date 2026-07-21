import { parentPort, workerData } from "node:worker_threads";

type LeaseWorkerData = {
  base_url: string;
  token: string;
  task_id: string;
  subject_id: string;
  fence_token: string;
};

const data = workerData as LeaseWorkerData;
let stopped = false;
let running = false;
let failures = 0;

function stopWith(type: "cancelled" | "lost") {
  if (stopped) return;
  stopped = true;
  clearInterval(timer);
  parentPort?.postMessage({ type });
}

async function heartbeat() {
  if (stopped || running) return;
  running = true;
  try {
    const response = await fetch(`${data.base_url}/internal/agent-runtime/v1/tasks/${data.task_id}/heartbeat`, {
      method: "POST",
      headers: { authorization: `Bearer ${data.token}`, "content-type": "application/json" },
      body: JSON.stringify({ subject_id: data.subject_id, fence_token: data.fence_token }),
    });
    if (response.status === 401 || response.status === 409) {
      stopWith("lost");
      return;
    }
    if (!response.ok) throw new Error("heartbeat rejected");
    const result = await response.json() as { cancel_requested?: boolean };
    failures = 0;
    if (result.cancel_requested) stopWith("cancelled");
  } catch {
    failures++;
    if (failures >= 3) stopWith("lost");
  } finally {
    running = false;
  }
}

const timer = setInterval(() => { void heartbeat(); }, 15_000);
parentPort?.on("message", (message: { type?: string }) => {
  if (message.type === "stop") {
    stopped = true;
    clearInterval(timer);
  }
});
