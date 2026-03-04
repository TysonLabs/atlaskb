import { BrowserRouter, Routes, Route } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { DashboardPage } from "./components/dashboard/DashboardPage";
import { ReposPage } from "./components/repos/ReposPage";
import { RepoDetail } from "./components/repos/RepoDetail";
import { EntityExplorerPage } from "./components/entities/EntityExplorerPage";
import { EntityDetailPage } from "./components/entities/EntityDetail";
import { AskPage } from "./components/ask/AskPage";
import { IndexingPage } from "./components/indexing/IndexingPage";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppShell />}>
          <Route path="/" element={<DashboardPage />} />
          <Route path="/repos" element={<ReposPage />} />
          <Route path="/repos/:id" element={<RepoDetail />} />
          <Route path="/indexing" element={<IndexingPage />} />
          <Route path="/entities" element={<EntityExplorerPage />} />
          <Route path="/entities/:id" element={<EntityDetailPage />} />
          <Route path="/ask" element={<AskPage />} />
          <Route path="/ask/:sessionId" element={<AskPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
