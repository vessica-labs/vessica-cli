import type { ButtonHTMLAttributes, PropsWithChildren, ReactNode } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  LoaderCircle,
  WifiOff,
} from "lucide-react";
import { titleCase } from "@/lib/api";

export function Button({
  className = "",
  variant = "primary",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "danger" | "ghost";
}) {
  return (
    <button className={`button button-${variant} ${className}`} {...props} />
  );
}
export function Card({
  children,
  className = "",
}: PropsWithChildren<{ className?: string }>) {
  return <section className={`card ${className}`}>{children}</section>;
}
export function Badge({ status }: { status?: string }) {
  const normalized = (status || "unknown").toLowerCase();
  return (
    <span className={`badge status-${normalized}`}>{titleCase(status)}</span>
  );
}
export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: string;
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div>
        {eyebrow && <p className="eyebrow">{eyebrow}</p>}
        <h1>{title}</h1>
        {description && <p className="page-description">{description}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  );
}
export function Empty({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="empty-state">
      <div className="empty-glyph">◇</div>
      <h3>{title}</h3>
      <p>{detail}</p>
    </div>
  );
}
export function Loading({ label = "Loading" }: { label?: string }) {
  return (
    <div className="state-line" role="status">
      <LoaderCircle className="spin" size={16} /> {label}
    </div>
  );
}
export function ErrorState({ error }: { error: unknown }) {
  return (
    <div className="notice notice-error" role="alert">
      <AlertTriangle size={18} />
      <span>
        {error instanceof Error ? error.message : "Something went wrong."}
      </span>
    </div>
  );
}
export function HealthIcon({ state }: { state: string }) {
  return state === "ready" ||
    state === "configured" ||
    state === "connected" ? (
    <CheckCircle2 className="ok" size={18} />
  ) : state === "unavailable" ? (
    <WifiOff className="bad" size={18} />
  ) : (
    <AlertTriangle className="warn" size={18} />
  );
}
