import { NavLink, Outlet } from "react-router-dom";
import {
  Activity,
  BookOpen,
  Box,
  Brain,
  Bot,
  Cloud,
  LayoutDashboard,
  Moon,
  ShieldCheck,
  Sun,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

const nav = [
  ["/", "Overview", LayoutDashboard],
  ["/runs", "Runs", Activity],
  ["/agents", "Agents", Bot],
  ["/sandboxes", "Sandboxes", Box],
  ["/knowledge", "Knowledge", Brain],
  ["/docs", "Documentation", BookOpen],
  ["/workspace", "Workspace", Cloud],
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
  const queryClient = useQueryClient();
  const repositories = useQuery({
    queryKey: ["layout-repositories"],
    queryFn: () => api<{ repositories: Array<{ id: string; display_name: string }> }>("/api/v1/system"),
  });
  const [repositoryID, setRepositoryID] = useState(localStorage.getItem("vessica-repository-id") || "");
  useEffect(() => {
    const available = repositories.data?.repositories || [];
    if (available.length && !available.some((repository) => repository.id === repositoryID)) {
      setRepositoryID(available[0].id);
      localStorage.setItem("vessica-repository-id", available[0].id);
    }
  }, [repositories.data, repositoryID]);
  const selectRepository = (value: string) => {
    setRepositoryID(value);
    localStorage.setItem("vessica-repository-id", value);
    void queryClient.invalidateQueries();
  };
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
        {!!repositories.data?.repositories?.length && (
          <label className="repository-switcher">
            <span>Repository</span>
            <select value={repositoryID} onChange={(event) => selectRepository(event.target.value)}>
              {repositories.data.repositories.map((repository) => (
                <option key={repository.id} value={repository.id}>{repository.display_name}</option>
              ))}
            </select>
          </label>
        )}
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
