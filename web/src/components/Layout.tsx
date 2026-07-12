import type { ReactNode } from "react";
import { useApp } from "../lib/app-context";
import { Link, useRouter } from "../lib/router";

const navItems = [
  { to: "/", label: "Apps", match: (path: string) => path === "/" || path.startsWith("/apps") },
  { to: "/jobs", label: "Jobs", match: (path: string) => path.startsWith("/jobs") },
  { to: "/settings", label: "Settings", match: (path: string) => path.startsWith("/settings") },
];

export function Layout({
  title,
  subtitle,
  actions,
  children,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const { path } = useRouter();
  const { settings, toasts, dismissToast } = useApp();

  return (
    <div className="appShell">
      <aside className="sidebar">
        <Link className="brand" to="/">
          <span className="brandMark" aria-hidden="true">
            ⌁
          </span>
          <span className="brandName">windforce-lite</span>
        </Link>
        <nav className="nav" aria-label="Primary">
          {navItems.map((item) => (
            <Link key={item.to} to={item.to} className={item.match(path) ? "navItem active" : "navItem"}>
              {item.label}
            </Link>
          ))}
        </nav>
        <div className="sidebarFooter">
          <span className="workspacePill" title="Active workspace">
            workspace / {settings.workspace}
          </span>
          <span className="actorPill" title="Audit actor for state-changing requests">
            actor / {settings.actor || "system"}
          </span>
        </div>
      </aside>
      <div className="mainArea">
        <header className="topbar">
          <div>
            <h1>{title}</h1>
            {subtitle ? <p className="topbarSubtitle">{subtitle}</p> : null}
          </div>
          {actions ? <div className="topbarActions">{actions}</div> : null}
        </header>
        <main className="content">{children}</main>
      </div>
      <div className="toastStack" aria-live="polite">
        {toasts.map((toast) => (
          <div key={toast.id} className={`toast toast-${toast.tone}`} id="toast">
            <span>{toast.text}</span>
            <button type="button" className="toastClose" aria-label="Dismiss" onClick={() => dismissToast(toast.id)}>
              ×
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
