import { NavLink, Outlet, useLocation } from "react-router-dom";
import {
  LayoutDashboard,
  FolderGit2,
  Boxes,
  MessageSquare,
  Network,
} from "lucide-react";

const navItems = [
  { to: "/", icon: LayoutDashboard, label: "Dashboard" },
  { to: "/repos", icon: FolderGit2, label: "Repos" },
  { to: "/entities", icon: Boxes, label: "Entities" },
  { to: "/ask", icon: MessageSquare, label: "Ask" },
  { to: "/graph", icon: Network, label: "Graph" },
];

export function AppShell() {
  const location = useLocation();
  const isAskPage = location.pathname.startsWith("/ask");

  return (
    <div className="flex h-screen bg-surface">
      <aside className="w-56 bg-sidebar flex flex-col shrink-0 border-r border-edge-subtle">
        <div className="p-4 border-b border-edge-subtle">
          <h1 className="text-lg font-bold tracking-tight text-foreground">AtlasKB</h1>
          <p className="text-xs text-foreground-muted mt-0.5">Knowledge Base</p>
        </div>
        <nav className="flex-1 py-2">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              className={({ isActive }) =>
                `flex items-center gap-3 px-4 py-2.5 text-sm transition-colors ${
                  isActive
                    ? "bg-sidebar-active/15 text-accent border-r-2 border-accent"
                    : "text-foreground-secondary hover:bg-sidebar-hover hover:text-foreground"
                }`
              }
            >
              <item.icon size={18} />
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <main className={`flex-1 ${isAskPage ? "overflow-hidden" : "overflow-auto"}`}>
        {isAskPage ? (
          <Outlet />
        ) : (
          <div className="p-6 max-w-7xl mx-auto">
            <Outlet />
          </div>
        )}
      </main>
    </div>
  );
}
