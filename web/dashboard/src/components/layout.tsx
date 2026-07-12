import { NavLink, Outlet } from "react-router-dom";
import {
  Activity,
  BookOpen,
  Box,
  Brain,
  Cloud,
  LayoutDashboard,
  Moon,
  ShieldCheck,
  Sun,
} from "lucide-react";
import { useEffect, useState } from "react";

const nav = [
  ["/", "Overview", LayoutDashboard],
  ["/runs", "Runs", Activity],
  ["/sandboxes", "Sandboxes", Box],
  ["/knowledge", "Knowledge", Brain],
  ["/docs", "Documentation", BookOpen],
  ["/hosting", "Hosting", Cloud],
  ["/access", "Access", ShieldCheck],
] as const;

function ThemeToggle() {
  const [theme, setTheme] = useState(
    localStorage.getItem("vessica-theme") || "system",
  );
  useEffect(() => {
    const dark =
      theme === "dark" ||
      (theme === "system" &&
        matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("vessica-theme", theme);
  }, [theme]);
  return (
    <button
      className="icon-button"
      aria-label={`Theme: ${theme}`}
      onClick={() =>
        setTheme(
          theme === "system" ? "light" : theme === "light" ? "dark" : "system",
        )
      }
    >
      {theme === "dark" ? <Moon size={17} /> : <Sun size={17} />}
    </button>
  );
}

export function Layout() {
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">V</div>
          <div>
            <strong>Vessica</strong>
            <span>Control surface</span>
          </div>
        </div>
        <nav aria-label="Primary">
          {nav.map(([to, label, Icon]) => (
            <NavLink key={to} to={to} end={to === "/"}>
              <Icon size={18} />
              <span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          <span>Dashboard v1</span>
          <ThemeToggle />
        </div>
      </aside>
      <div className="workspace">
        <div className="mobile-bar">
          <div className="brand">
            <div className="brand-mark">V</div>
            <strong>Vessica</strong>
          </div>
          <ThemeToggle />
        </div>
        <main id="main">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
