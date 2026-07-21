import { createServer } from "node:http";
import { ControlPlaneClient } from "./control-plane.js";
import { OpenAIAgentsExecutor } from "./executor.js";
import { FakeExecutor } from "./fake-executor.js";
import { workerLeaseFactory } from "./lease.js";
import { Runtime } from "./runtime.js";

const baseURL = process.env.VES_CONTROL_PLANE_INTERNAL_URL?.replace(/\/$/, "");
const token = process.env.VES_AGENT_RUNTIME_TOKEN;
const port = Number(process.env.PORT || 8080);
const fakeProvider = process.env.VES_AGENT_RUNTIME_FAKE_PROVIDER === "1";
if (!baseURL || !token) throw new Error("VES_CONTROL_PLANE_INTERNAL_URL and VES_AGENT_RUNTIME_TOKEN are required");

const client = new ControlPlaneClient(baseURL, token);
const executor = fakeProvider ? new FakeExecutor(client) : new OpenAIAgentsExecutor(client, workerLeaseFactory);
const credentialsReady = fakeProvider || !!process.env.OPENAI_API_KEY;
const runtime = new Runtime(client, executor, Number(process.env.VES_AGENT_RUNTIME_CONCURRENCY || 4), credentialsReady, workerLeaseFactory);
let ready = false;
const server = createServer((request, response) => {
  response.setHeader("content-type", "application/json");
  if (request.url === "/healthz") { response.end(JSON.stringify({ ok: true, service: "vessica-agent-runtime" })); return; }
  if (request.url === "/readyz") { response.statusCode = ready ? 200 : 503; response.end(JSON.stringify({ ok: ready, credentials_ready: credentialsReady, provider: fakeProvider ? "fake" : "openai" })); return; }
  response.statusCode = 404; response.end(JSON.stringify({ error: "not found" }));
});
server.listen(port, "0.0.0.0", () => { ready = credentialsReady; });
void runtime.start().catch(() => { ready = false; process.stderr.write("agent-runtime stopped after a control-plane or capability error\n"); });
for (const signal of ["SIGINT", "SIGTERM"] as const) process.on(signal, () => { runtime.stop(); server.close(); });
