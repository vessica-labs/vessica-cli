import type { ControlPlaneClient } from "./control-plane.js";
import type { NormalizedEvent } from "./contracts.js";

export class EventBatcher {
  private ordinal = 0;
  private pending: NormalizedEvent[] = [];
  private bytes = 0;
  private timer?: NodeJS.Timeout;
  constructor(private readonly client: ControlPlaneClient, private readonly runID: string, private readonly fence: string) {}

  async append(type: string, payload: unknown) {
    if (type === "agent.message.delta" && this.pending.at(-1)?.type === type) {
      const current = this.pending.at(-1)!.payload as { text?: string };
      const next = payload as { text?: string };
      current.text = (current.text ?? "") + (next.text ?? "");
      this.bytes += Buffer.byteLength(next.text ?? "");
      if (this.bytes >= 2048) await this.flush();
      return;
    }
    const event = { ordinal: ++this.ordinal, type, payload };
    this.pending.push(event);
    this.bytes += Buffer.byteLength(JSON.stringify(payload));
    if (this.bytes >= 2048) await this.flush();
    else if (!this.timer) this.timer = setTimeout(() => void this.flush().catch(() => undefined), 500);
  }
  async flush() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = undefined;
    if (!this.pending.length) return;
    const batch = this.pending;
    this.pending = [];
    this.bytes = 0;
    try {
      await this.client.events(this.runID, this.fence, batch);
    } catch (error) {
      this.pending = [...batch, ...this.pending];
      this.bytes = this.pending.reduce((total, event) => total + Buffer.byteLength(JSON.stringify(event.payload)), 0);
      throw error;
    }
  }
}
