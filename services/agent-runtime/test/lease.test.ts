import { afterEach, describe, expect, it, vi } from "vitest";
import type { ControlPlaneClient } from "../src/control-plane.js";
import type { ClaimedTask } from "../src/contracts.js";
import { intervalLeaseFactory } from "../src/lease.js";

const task = {
  protocol: "vessica.agent-runtime/v1",
  fence_token: "fence_1",
  task: { id: "atask_1", kind: "build", subject_id: "abuild_1", attempts: 1 },
  build: { id: "abuild_1", kind: "create", description: "build" },
} satisfies ClaimedTask;

describe("lease heartbeat", () => {
  afterEach(() => vi.useRealTimers());

  it("renews until stopped", async () => {
    vi.useFakeTimers();
    const client = { heartbeat: vi.fn().mockResolvedValue({ cancel_requested: false }) } as unknown as ControlPlaneClient;
    const abort = vi.fn();
    const lease = intervalLeaseFactory(client, task, abort);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(client.heartbeat).toHaveBeenCalledTimes(2);
    expect(abort).not.toHaveBeenCalled();
    lease.stop();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(client.heartbeat).toHaveBeenCalledTimes(2);
  });

  it("does not abandon a lease on one transient heartbeat failure", async () => {
    vi.useFakeTimers();
    const client = { heartbeat: vi.fn().mockRejectedValue(new Error("network")) } as unknown as ControlPlaneClient;
    const abort = vi.fn();
    const lease = intervalLeaseFactory(client, task, abort);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(abort).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(15_000);
    expect(abort).toHaveBeenCalledWith(expect.objectContaining({ message: "attempt lease lost" }));
    lease.stop();
  });
});
