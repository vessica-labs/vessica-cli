import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { Layout } from "@/components/layout";
import { Overview } from "@/pages/overview";
import { Runs } from "@/pages/runs";
import { RunDetail } from "@/pages/run-detail";
import { Sandboxes } from "@/pages/sandboxes";
import { Knowledge, KnowledgeDetail } from "@/pages/knowledge";
import { Docs, Doc } from "@/pages/docs";
import { Workspace } from "@/pages/hosting";
import { Access } from "@/pages/access";
import { Agents, AgentDetail, AgentRun } from "@/pages/agents";
const client = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 3000 } },
});
export function App() {
  return (
    <QueryClientProvider client={client}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Overview />} />
            <Route path="runs" element={<Runs />} />
            <Route path="runs/:id" element={<RunDetail />} />
            <Route path="agents" element={<Agents />} />
            <Route path="agents/:id" element={<AgentDetail />} />
            <Route path="agent-runs/:id" element={<AgentRun />} />
            <Route path="sandboxes" element={<Sandboxes />} />
            <Route path="knowledge" element={<Knowledge />} />
            <Route path="knowledge/:type/:id" element={<KnowledgeDetail />} />
            <Route path="docs" element={<Docs />} />
            <Route path="docs/:slug" element={<Doc />} />
            <Route path="workspace" element={<Workspace />} />
            <Route path="access" element={<Access />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
