import {
  Activity,
  AppWindow,
  ArrowLeft,
  ChevronDown,
  ContactRound,
  Eraser,
  KeyRound,
  Menu,
  MonitorSmartphone,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  PanelTopOpen,
  ScrollText,
  Settings,
  Sun,
  Wind,
  X,
} from "lucide-react";
import { Dialog as DialogPrimitive, DropdownMenu as DropdownMenuPrimitive } from "radix-ui";
import { type ReactNode, useEffect, useState } from "react";
import { useApp } from "../lib/app-context";
import { Link, useRouter } from "../lib/router";
import { type HostConsoleConfig, loadHostConsoleConfig } from "../lib/runtime-config";
import { cn } from "../shared/lib/cn";
import { useThemeStore } from "../shared/lib/theme";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";

export const primaryNavItems = [
  {
    to: "/",
    label: "Apps",
    icon: AppWindow,
    match: (path: string) => path === "/" || path.startsWith("/apps"),
  },
  {
    to: "/clients",
    label: "Client Registry",
    icon: ContactRound,
    match: (path: string) => path.startsWith("/clients"),
  },
  {
    to: "/monitoring",
    label: "Monitoring",
    icon: Activity,
    match: (path: string) => path.startsWith("/monitoring") || path.startsWith("/jobs"),
  },
  {
    to: "/audit",
    label: "Audit",
    icon: ScrollText,
    match: (path: string) => path.startsWith("/audit"),
  },
  {
    to: "/settings",
    label: "Settings",
    icon: Settings,
    match: (path: string) => path.startsWith("/settings") && path !== "/settings/workspaces",
  },
];

function loadCollapsed(): boolean {
  return globalThis.localStorage?.getItem("wf.sidebarCollapsed") === "true";
}

function ThemeToggle() {
  const preference = useThemeStore((state) => state.preference);
  const cycle = useThemeStore((state) => state.cycle);
  const Icon = preference === "light" ? Sun : preference === "dark" ? Moon : MonitorSmartphone;
  const label = preference === "light" ? "light" : preference === "dark" ? "dark" : "system";
  return (
    <button
      type="button"
      className="icon-control"
      onClick={cycle}
      title={`Theme: ${label}`}
      aria-label={`Change theme (current: ${label})`}
    >
      <Icon size={16} />
    </button>
  );
}

function HostConsoleAction({
  placement = "topbar",
  collapsed = false,
}: {
  placement?: "topbar" | "sidebar";
  collapsed?: boolean;
}) {
  const [hostConsole, setHostConsole] = useState<HostConsoleConfig | null>(null);

  useEffect(() => {
    let active = true;
    void loadHostConsoleConfig()
      .then((config) => {
        if (active) setHostConsole(config);
      })
      .catch(() => {
        if (active) setHostConsole(null);
      });
    return () => {
      active = false;
    };
  }, []);

  if (!hostConsole) return null;
  return (
    <a
      className={cn(
        placement === "sidebar"
          ? "flex min-h-10 w-full min-w-0 items-center gap-3 rounded-lg border border-shell-border bg-shell-foreground/5 px-3 text-sm font-medium text-shell-foreground no-underline transition-colors hover:bg-shell-foreground/10"
          : "button secondary small min-w-0 gap-2 no-underline",
        placement === "sidebar" && collapsed && "justify-center px-0",
      )}
      data-testid="host-console-action"
      href={hostConsole.url}
      aria-label={hostConsole.label}
      title={hostConsole.label}
    >
      <PanelTopOpen size={15} aria-hidden="true" />
      {placement === "sidebar" ? (
        collapsed ? null : (
          <span className="truncate">{hostConsole.label}</span>
        )
      ) : (
        <span className="hidden max-w-48 truncate lg:inline">{hostConsole.label}</span>
      )}
    </a>
  );
}

export function UserMenu({
  placement = "topbar",
  collapsed = false,
}: {
  placement?: "topbar" | "sidebar";
  collapsed?: boolean;
} = {}) {
  const { settings, clearLocalCredentials, notify } = useApp();
  const { navigate } = useRouter();
  const hasApiToken = Boolean(settings.token);
  const hasBrowserIdentity = Boolean(settings.actor || settings.token);

  function handleClearLocalCredentials() {
    clearLocalCredentials();
    navigate("/settings");
    notify("info", "Browser API token and audit actor cleared.");
  }

  const itemClass =
    "flex cursor-pointer select-none items-center gap-2 rounded px-2 py-2 text-sm outline-none data-[disabled]:cursor-not-allowed data-[disabled]:opacity-45 data-[highlighted]:bg-muted";

  return (
    <DropdownMenuPrimitive.Root modal={false}>
      <DropdownMenuPrimitive.Trigger asChild>
        <button
          type="button"
          className={cn(
            placement === "sidebar"
              ? "flex min-h-10 w-full min-w-0 items-center gap-2.5 rounded-lg px-2 text-left text-shell-foreground hover:bg-shell-foreground/10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-shell-active"
              : "flex min-w-0 items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary",
            placement === "sidebar" && collapsed && "justify-center px-0",
          )}
          aria-label={`Browser access menu for ${settings.actor || "system"}`}
        >
          <KeyRound
            className={cn(
              "shrink-0",
              placement === "sidebar" ? "text-shell-muted-foreground" : "text-muted-foreground",
            )}
            size={18}
          />
          <span
            className={cn(
              "min-w-0 flex-1",
              placement === "topbar" && "hidden sm:block",
              collapsed && "hidden",
            )}
          >
            <span className="block truncate text-sm font-medium leading-tight">Browser access</span>
            <span
              className={cn(
                "block text-xs leading-tight",
                placement === "sidebar" ? "text-shell-muted-foreground" : "text-muted-foreground",
              )}
            >
              {settings.actor || "system"} actor
            </span>
          </span>
          {collapsed ? null : (
            <ChevronDown
              className={cn(
                "shrink-0",
                placement === "sidebar" ? "text-shell-muted-foreground" : "text-muted-foreground",
              )}
              size={14}
            />
          )}
        </button>
      </DropdownMenuPrimitive.Trigger>
      <DropdownMenuPrimitive.Portal>
        <DropdownMenuPrimitive.Content
          align={placement === "sidebar" ? "start" : "end"}
          side={placement === "sidebar" ? "top" : "bottom"}
          sideOffset={8}
          className="z-[100] min-w-56 rounded-md border border-border bg-surface p-1 text-foreground shadow-lg"
        >
          <DropdownMenuPrimitive.Label className="px-2 py-2">
            <span className="block text-sm font-medium">Browser access</span>
            <span className="block text-xs text-muted-foreground">
              {settings.actor || "system"} actor ·{" "}
              {hasApiToken ? "API token configured" : "API token not configured"}
            </span>
          </DropdownMenuPrimitive.Label>
          <DropdownMenuPrimitive.Separator className="my-1 h-px bg-border" />
          <DropdownMenuPrimitive.Item className={itemClass} onSelect={() => navigate("/settings")}>
            <Settings size={16} />
            Connection settings
          </DropdownMenuPrimitive.Item>
          <DropdownMenuPrimitive.Item
            className={itemClass}
            disabled={!hasBrowserIdentity}
            onSelect={handleClearLocalCredentials}
          >
            <Eraser size={16} />
            {hasBrowserIdentity ? "Clear browser access" : "No browser access configured"}
          </DropdownMenuPrimitive.Item>
        </DropdownMenuPrimitive.Content>
      </DropdownMenuPrimitive.Portal>
    </DropdownMenuPrimitive.Root>
  );
}

function MobileNavigation({ path }: { path: string }) {
  const [open, setOpen] = useState(false);

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Trigger asChild>
        <button
          className="mobileNavTrigger icon-control"
          type="button"
          aria-label="Open navigation menu"
        >
          <Menu size={18} aria-hidden="true" />
        </button>
      </DialogPrimitive.Trigger>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[80] bg-[var(--overlay)] md:hidden" />
        <DialogPrimitive.Content
          className="platformMobileNav fixed inset-y-0 left-0 z-[81] flex w-[min(20rem,calc(100vw-3rem))] flex-col border-r border-shell-border bg-shell-background text-shell-foreground shadow-md outline-none md:hidden"
          aria-describedby={undefined}
        >
          <header className="flex h-[var(--shell-header-height)] shrink-0 items-center justify-between border-b border-shell-border px-4">
            <DialogPrimitive.Title className="flex items-center gap-2 text-sm font-semibold">
              <span className="flex size-8 items-center justify-center rounded-lg bg-shell-active text-shell-active-foreground">
                <Wind size={16} strokeWidth={2.2} aria-hidden="true" />
              </span>
              <span className="min-w-0">
                <span className="block leading-tight">Windforce</span>
                <span className="block text-xs font-normal leading-tight text-shell-muted-foreground">
                  Execution workspace
                </span>
              </span>
            </DialogPrimitive.Title>
            <DialogPrimitive.Close className="icon-control" aria-label="Close navigation menu">
              <X size={17} aria-hidden="true" />
            </DialogPrimitive.Close>
          </header>
          <section className="border-b border-shell-border p-3" aria-label="Workspace context">
            <span className="mb-2 block px-1 text-[0.6875rem] font-medium uppercase tracking-[0.08em] text-shell-muted-foreground">
              Workspace
            </span>
            <WorkspaceSwitcher />
          </section>
          <nav className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-4" aria-label="Mobile">
            {primaryNavItems.map((item) => {
              const Icon = item.icon;
              const active = item.match(path);
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  className={cn("navItem", active && "active")}
                  onClick={() => setOpen(false)}
                >
                  <Icon size={17} strokeWidth={1.9} aria-hidden="true" />
                  <span>{item.label}</span>
                </Link>
              );
            })}
          </nav>
          <footer className="grid gap-2 border-t border-shell-border p-3">
            <HostConsoleAction placement="sidebar" />
            <UserMenu placement="sidebar" />
          </footer>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function Layout({
  title,
  subtitle,
  actions,
  children,
  scope = "workspace",
  titleLeading,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
  children: ReactNode;
  scope?: "workspace" | "instance";
  titleLeading?: ReactNode;
}) {
  const { path } = useRouter();
  const { settings, toasts, dismissToast } = useApp();
  const [collapsed, setCollapsed] = useState(loadCollapsed);

  useEffect(() => {
    globalThis.localStorage?.setItem("wf.sidebarCollapsed", String(collapsed));
  }, [collapsed]);

  if (scope === "instance") {
    return (
      <div className="min-h-screen bg-background text-foreground">
        <header className="flex h-[var(--shell-header-height)] items-center justify-between border-b border-border bg-background px-4 sm:px-6">
          <div className="flex items-center gap-3">
            <Link
              className="flex items-center gap-2 text-sm font-semibold text-foreground no-underline"
              to="/"
            >
              <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <Wind size={16} strokeWidth={2.2} />
              </span>
              windforce-core
            </Link>
            <span className="h-5 w-px bg-border" aria-hidden="true" />
            <span className="text-xs font-medium text-muted-foreground">
              Workspace administration
            </span>
          </div>
          <div className="flex items-center gap-2">
            <HostConsoleAction />
            <ThemeToggle />
            <UserMenu />
            <Link
              className="button small"
              to="/"
              aria-label="Open current workspace"
              title="Open current workspace"
            >
              <ArrowLeft size={15} />
              <span className="hidden sm:inline">Open current workspace</span>
            </Link>
          </div>
        </header>
        <main className="mx-auto w-full max-w-[var(--content-max-width)] px-4 py-6 sm:px-6">
          <PageHeading
            title={title}
            subtitle={subtitle}
            actions={actions}
            titleLeading={titleLeading}
          />
          <div className="mt-6 flex flex-col gap-4">{children}</div>
        </main>
        <ToastStack toasts={toasts} dismissToast={dismissToast} />
      </div>
    );
  }

  return (
    <div className="flex min-h-screen bg-background text-foreground">
      <aside
        className={cn(
          "platformSidebar appSidebar hidden h-screen shrink-0 flex-col border-r border-shell-border bg-shell-background text-shell-foreground transition-[width] duration-150 md:sticky md:top-0 md:flex",
          collapsed && "sidebarCollapsed",
          collapsed ? "w-16" : "w-[var(--shell-sidebar-width)]",
        )}
      >
        <div className="flex h-[var(--shell-header-height)] items-center gap-2 border-b border-shell-border px-4">
          <Link
            className={cn(
              "brand flex min-w-0 flex-1 items-center gap-3 text-sm font-semibold text-shell-foreground no-underline",
              collapsed && "justify-center",
            )}
            to="/"
            title="windforce-core"
          >
            <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-shell-active text-shell-active-foreground">
              <Wind size={16} strokeWidth={2.2} />
            </span>
            {!collapsed ? (
              <span className="min-w-0">
                <span className="block truncate leading-tight">Windforce</span>
                <span className="block truncate text-xs font-normal leading-tight text-shell-muted-foreground">
                  Execution workspace
                </span>
              </span>
            ) : null}
          </Link>
        </div>
        <section
          className={cn(
            "sidebarWorkspaceContext border-b border-shell-border p-3",
            collapsed && "px-2",
          )}
          aria-label="Workspace context"
        >
          {!collapsed ? (
            <span className="mb-2 block px-1 text-[0.6875rem] font-medium uppercase tracking-[0.08em] text-shell-muted-foreground">
              Workspace
            </span>
          ) : null}
          <WorkspaceSwitcher />
        </section>
        <nav className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-4" aria-label="Primary">
          {primaryNavItems.map((item) => {
            const Icon = item.icon;
            const active = item.match(path);
            return (
              <Link
                key={item.to}
                to={item.to}
                className={cn("navItem", active && "active", collapsed && "justify-center px-0")}
                title={item.label}
              >
                <Icon size={17} strokeWidth={1.9} aria-hidden="true" />
                {!collapsed ? <span>{item.label}</span> : null}
              </Link>
            );
          })}
        </nav>
        <footer
          className={cn(
            "sidebarFooter grid gap-2 border-t border-shell-border p-3",
            collapsed && "px-2",
          )}
        >
          <HostConsoleAction placement="sidebar" collapsed={collapsed} />
          <UserMenu placement="sidebar" collapsed={collapsed} />
        </footer>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-[var(--shell-header-height)] shrink-0 items-center justify-between gap-3 border-b border-border bg-background px-4 sm:px-6">
          <div className="flex min-w-0 items-center gap-2">
            <MobileNavigation path={path} />
            <button
              className="icon-control hidden md:inline-flex"
              id="sidebarToggle"
              type="button"
              aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
              aria-expanded={!collapsed}
              title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
              onClick={() => setCollapsed((current) => !current)}
            >
              {collapsed ? <PanelLeftOpen size={17} /> : <PanelLeftClose size={17} />}
            </button>
            <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
              <span className="hidden font-mono text-foreground sm:inline">
                {settings.workspace}
              </span>
              <span className="hidden sm:inline" aria-hidden="true">
                workspace
              </span>
              <span aria-hidden="true">/</span>
              <span className="truncate font-medium text-foreground">{title}</span>
            </div>
          </div>
          <div className="flex shrink-0 items-center">
            <ThemeToggle />
          </div>
        </header>
        <main className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-[var(--content-max-width)] px-4 py-6 sm:px-6">
            <PageHeading
              title={title}
              subtitle={subtitle}
              actions={actions}
              titleLeading={titleLeading}
            />
            <div className="mt-6 flex flex-col gap-4">{children}</div>
          </div>
        </main>
      </div>
      <ToastStack toasts={toasts} dismissToast={dismissToast} />
    </div>
  );
}

function PageHeading({
  title,
  subtitle,
  actions,
  titleLeading,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
  titleLeading?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="flex min-w-0 items-start gap-3">
        {titleLeading}
        <div className="min-w-0">
          <h1 className="text-xl font-semibold text-balance">{title}</h1>
          {subtitle ? (
            <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{subtitle}</p>
          ) : null}
        </div>
      </div>
      {actions ? (
        <div className="topbarActions flex flex-wrap items-center gap-2">{actions}</div>
      ) : null}
    </div>
  );
}

function ToastStack({
  toasts,
  dismissToast,
}: {
  toasts: Array<{ id: number; text: string; tone: string }>;
  dismissToast: (id: number) => void;
}) {
  return (
    <div className="toastStack" aria-live="polite">
      {toasts.map((toast) => (
        <div key={toast.id} className={`toast toast-${toast.tone}`} id="toast">
          <span>{toast.text}</span>
          <button
            type="button"
            className="icon-control"
            aria-label="Dismiss"
            onClick={() => dismissToast(toast.id)}
          >
            <X size={15} />
          </button>
        </div>
      ))}
    </div>
  );
}
