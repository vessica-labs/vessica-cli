export type Envelope<T> = {
  schema: string;
  data?: T;
  meta?: Record<string, unknown>;
  error?: { code: string; message: string; request_id: string };
};

let csrfToken = sessionStorage.getItem("vessica-csrf") || "";
export const setCSRF = (value: string) => {
  csrfToken = value;
  sessionStorage.setItem("vessica-csrf", value);
};

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method || "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const repositoryID = localStorage.getItem("vessica-repository-id");
  if (repositoryID) headers.set("X-Vessica-Repository-ID", repositoryID);
  if (init.body) headers.set("Content-Type", "application/json");
  if (!["GET", "HEAD"].includes(method)) {
    headers.set("X-CSRF-Token", csrfToken);
    headers.set("Idempotency-Key", crypto.randomUUID());
  }
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin",
  });
  const envelope = (await response.json()) as Envelope<T>;
  if (!response.ok || envelope.error)
    throw new Error(
      envelope.error?.message || `Request failed (${response.status})`,
    );
  return envelope.data as T;
}

export async function bootstrapSession() {
  const params = new URLSearchParams(location.hash.slice(1));
  const launch = params.get("launch_token");
  if (launch) {
    const result = await api<{ csrf_token: string }>("/auth/local/exchange", {
      method: "POST",
      body: JSON.stringify({ token: launch }),
    });
    setCSRF(result.csrf_token);
    history.replaceState(null, "", location.pathname + location.search);
  }
  return api<{
    user_id: string;
    role: "owner" | "member";
    mode: "local" | "hosted";
  }>("/auth/session");
}

export const fmtTime = (value?: string) =>
  value
    ? new Intl.DateTimeFormat(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date(value))
    : "—";
export const titleCase = (value?: string) =>
  (value || "unknown")
    .replaceAll("_", " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
