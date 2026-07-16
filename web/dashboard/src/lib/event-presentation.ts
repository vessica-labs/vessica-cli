export type EventPresentation = {
  agentMessage: boolean;
  title: string;
  summary: string;
  message: string;
  detail: string;
};

const words = (value: string) =>
  value
    .replace(/[._-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());

const string = (value: unknown) => (typeof value === "string" ? value : "");

export function presentEvent(event: any): EventPresentation {
  const payload = event?.payload || {};
  const type = string(event?.type) || "event";
  const message = string(payload.message);
  const agentMessage = type === "agent.message" || type === "agent.output";
  if (agentMessage) {
    return {
      agentMessage: true,
      title: string(payload.role) ? `${words(string(payload.role))} message` : "Agent message",
      summary: "",
      message,
      detail: "",
    };
  }

  const phase = string(payload.phase);
  const step = string(payload.step);
  const status = string(payload.status);
  const summary =
    message ||
    step ||
    [phase && words(phase), status && words(status)].filter(Boolean).join(" · ") ||
    words(type);
  return {
    agentMessage: false,
    title: words(type),
    summary,
    message: "",
    detail: JSON.stringify(payload, null, 2),
  };
}
