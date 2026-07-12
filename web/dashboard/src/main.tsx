import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { Code2 as Github } from "lucide-react";
import { App } from "./app";
import { api, bootstrapSession, setCSRF } from "@/lib/api";
import { Button, ErrorState, Loading } from "@/components/ui";
import "./styles.css";

type AuthConfig = { mode: "local" | "hosted"; github_configured: boolean };
type Device = {
  id: string;
  user_code: string;
  verification_uri: string;
  interval: number;
};

function HostedSignIn({ config }: { config: AuthConfig }) {
  const [device, setDevice] = useState<Device>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  async function begin() {
    setBusy(true);
    setError(undefined);
    try {
      const query = new URLSearchParams(location.search);
      const value = await api<Device>("/auth/github/device", {
        method: "POST",
        body: JSON.stringify({
          owner_claim: query.get("owner_claim") || "",
          invitation: query.get("invitation") || "",
        }),
      });
      setDevice(value);
      window.open(value.verification_uri, "_blank", "noopener,noreferrer");
      poll(value);
    } catch (e) {
      setError(e);
      setBusy(false);
    }
  }
  function poll(flow: Device) {
    const tick = async () => {
      try {
        const value = await api<{ status: string; csrf_token?: string }>(
          `/auth/github/device/${flow.id}/poll`,
          { method: "POST", body: "{}" },
        );
        if (value.status === "completed" && value.csrf_token) {
          setCSRF(value.csrf_token);
          location.href = "/";
          return;
        }
      } catch (e) {
        setError(e);
        setBusy(false);
        return;
      }
      setTimeout(tick, Math.max(flow.interval, 5) * 1000);
    };
    setTimeout(tick, Math.max(flow.interval, 5) * 1000);
  }
  return (
    <div className="bootstrap">
      <div className="brand-mark large">V</div>
      <p className="eyebrow">Hosted workspace</p>
      <h1>Sign in to Vessica</h1>
      <p>
        Use your invited GitHub identity. Vessica uses the token only to verify
        who you are, then discards it.
      </p>
      {device ? (
        <div className="card">
          <span>Enter this code on GitHub</span>
          <h2>{device.user_code}</h2>
          <a href={device.verification_uri} target="_blank">
            Open GitHub device login
          </a>
          <Loading label="Waiting for authorization" />
        </div>
      ) : (
        <Button disabled={!config.github_configured || busy} onClick={begin}>
          <Github size={17} /> Continue with GitHub
        </Button>
      )}
      {!config.github_configured && (
        <div className="notice notice-error">
          GitHub OAuth is not configured for this deployment.
        </div>
      )}
      {error !== undefined && <ErrorState error={error} />}
    </div>
  );
}

function Bootstrap() {
  const [state, setState] = useState<"loading" | "ready" | "signin" | "error">(
    "loading",
  );
  const [error, setError] = useState<unknown>();
  const [config, setConfig] = useState<AuthConfig>();
  useEffect(() => {
    bootstrapSession()
      .then(() => setState("ready"))
      .catch(async (e: unknown) => {
        try {
          const c = await api<AuthConfig>("/auth/config");
          setConfig(c);
          if (c.mode === "hosted") {
            setState("signin");
            return;
          }
        } catch {}
        setError(e);
        setState("error");
      });
  }, []);
  if (state === "loading")
    return (
      <div className="bootstrap">
        <div className="brand-mark large">V</div>
        <Loading label="Opening workspace" />
      </div>
    );
  if (state === "signin" && config) return <HostedSignIn config={config} />;
  if (state === "error")
    return (
      <div className="bootstrap">
        <div className="brand-mark large">V</div>
        <h1>Dashboard session required</h1>
        <p>
          Open this dashboard with <code>ves dashboard --open</code> to
          establish a secure local session.
        </p>
        <ErrorState error={error} />
        <Button onClick={() => location.reload()}>Try again</Button>
      </div>
    );
  return <App />;
}
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Bootstrap />
  </StrictMode>,
);
