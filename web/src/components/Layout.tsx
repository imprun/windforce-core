import {
  Activity,
  AppWindow,
  Boxes,
  ChevronDown,
  CircleHelp,
  CircleUserRound,
  ContactRound,
  Eraser,
  Globe2,
  KeyRound,
  LogOut,
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
import { LocalBrowserAccessDialog } from "../features/AccessSettings";
import { useApp } from "../lib/app-context";
import { type HostAccount, loadHostAccount } from "../lib/host-account";
import { Link, useRouter } from "../lib/router";
import type { HostAccountConfig, HostConsoleConfig } from "../lib/runtime-config";
import { type Locale, setLocale, translate, useLocale } from "../shared/i18n";
import type { TranslationKey } from "../shared/i18n/resources";
import { cn } from "../shared/lib/cn";
import { useThemeStore } from "../shared/lib/theme";
import { WorkspaceSwitcher } from "./WorkspaceSwitcher";

export const primaryNavItems = [
  {
    to: "/",
    labelKey: "navigation.apps" as TranslationKey,
    icon: AppWindow,
    match: (path: string) => path === "/" || path.startsWith("/apps"),
  },
  {
    to: "/clients",
    labelKey: "navigation.clientRegistry" as TranslationKey,
    icon: ContactRound,
    match: (path: string) => path.startsWith("/clients"),
  },
  {
    to: "/worker-groups",
    labelKey: "navigation.workerGroups" as TranslationKey,
    icon: Boxes,
    match: (path: string) => path.startsWith("/worker-groups"),
  },
  {
    to: "/monitoring",
    labelKey: "navigation.monitoring" as TranslationKey,
    icon: Activity,
    match: (path: string) => path.startsWith("/monitoring") || path.startsWith("/jobs"),
  },
  {
    to: "/human-tasks",
    labelKey: "navigation.humanTasks" as TranslationKey,
    icon: CircleHelp,
    match: (path: string) => path.startsWith("/human-tasks"),
  },
  {
    to: "/audit",
    labelKey: "navigation.audit" as TranslationKey,
    icon: ScrollText,
    match: (path: string) => path.startsWith("/audit"),
  },
  {
    to: "/settings",
    labelKey: "navigation.settings" as TranslationKey,
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
  const label =
    preference === "light"
      ? translate("shell.themeLight")
      : preference === "dark"
        ? translate("shell.themeDark")
        : translate("shell.themeSystem");
  return (
    <button
      type="button"
      className="icon-control"
      onClick={cycle}
      title={translate("shell.theme", { theme: label })}
      aria-label={translate("shell.changeTheme", { theme: label })}
    >
      <Icon size={16} />
    </button>
  );
}

function LocaleSwitcher() {
  const locale = useLocale();
  const nextLocale: Locale = locale === "ko" ? "en" : "ko";
  const nextLanguage =
    nextLocale === "ko" ? translate("language.korean") : translate("language.english");
  const currentLanguageLabel = locale === "ko" ? translate("language.korean") : "EN";

  function changeLocale(nextLocale: Locale) {
    void setLocale(nextLocale);
  }

  return (
    <button
      type="button"
      className="icon-control locale-control"
      aria-label={translate("language.changeTo", { language: nextLanguage })}
      title={translate("language.changeTo", { language: nextLanguage })}
      onClick={() => changeLocale(nextLocale)}
    >
      <Globe2 size={16} aria-hidden="true" />
      <span aria-hidden="true">{currentLanguageLabel}</span>
    </button>
  );
}

function HostConsoleAction({ hostConsole }: { hostConsole: HostConsoleConfig | null }) {
  if (!hostConsole) return null;
  return (
    <a
      className="button secondary small hostConsoleAction min-w-0 no-underline"
      data-testid="host-console-action"
      href={hostConsole.url}
      aria-label={hostConsole.label}
      title={hostConsole.label}
    >
      <PanelTopOpen size={15} aria-hidden="true" />
      <span className="hidden max-w-48 truncate lg:inline">{hostConsole.label}</span>
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
  const { settings, clearLocalCredentials, notify, requestLocalAccess, runtimeConfig } = useApp();
  const hasApiToken = Boolean(settings.token);
  const hasBrowserIdentity = Boolean(settings.actor || settings.token);

  function handleClearLocalCredentials() {
    clearLocalCredentials();
    notify("info", translate("shell.localAccessCleared"));
  }

  const itemClass =
    "flex cursor-pointer select-none items-center gap-2 rounded px-2 py-2 text-sm outline-none data-[disabled]:cursor-not-allowed data-[disabled]:opacity-45 data-[highlighted]:bg-muted";

  if (runtimeConfig?.authMode !== "browser_token") return null;

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
          aria-label={translate("shell.localAccessMenu", {
            actor: settings.actor || "system",
          })}
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
            <span className="block truncate text-sm font-medium leading-tight">
              {translate("shell.localAccess")}
            </span>
            <span
              className={cn(
                "block text-xs leading-tight",
                placement === "sidebar" ? "text-shell-muted-foreground" : "text-muted-foreground",
              )}
            >
              {translate("shell.actor", { actor: settings.actor || "system" })}
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
            <span className="block text-sm font-medium">{translate("shell.localAccess")}</span>
            <span className="block text-xs text-muted-foreground">
              {translate("shell.actor", { actor: settings.actor || "system" })} ·{" "}
              {hasApiToken
                ? translate("shell.apiTokenConfigured")
                : translate("shell.apiTokenNotConfigured")}
            </span>
          </DropdownMenuPrimitive.Label>
          <DropdownMenuPrimitive.Separator className="my-1 h-px bg-border" />
          <DropdownMenuPrimitive.Item className={itemClass} onSelect={requestLocalAccess}>
            <Settings size={16} />
            {translate("shell.connectionSettings")}
          </DropdownMenuPrimitive.Item>
          <DropdownMenuPrimitive.Item
            className={itemClass}
            disabled={!hasBrowserIdentity}
            onSelect={handleClearLocalCredentials}
          >
            <Eraser size={16} />
            {hasBrowserIdentity
              ? translate("shell.clearLocalAccess")
              : translate("shell.noLocalAccess")}
          </DropdownMenuPrimitive.Item>
        </DropdownMenuPrimitive.Content>
      </DropdownMenuPrimitive.Portal>
    </DropdownMenuPrimitive.Root>
  );
}

function HostedAccountMenu({
  config,
  placement = "topbar",
  collapsed = false,
}: {
  config: HostAccountConfig;
  placement?: "topbar" | "sidebar";
  collapsed?: boolean;
}) {
  const [account, setAccount] = useState<HostAccount | null>(null);
  const [settled, setSettled] = useState(false);

  useEffect(() => {
    let active = true;
    setSettled(false);
    void loadHostAccount(config.endpoint)
      .then((result) => {
        if (active) setAccount(result);
      })
      .catch(() => {
        if (active) setAccount(null);
      })
      .finally(() => {
        if (active) setSettled(true);
      });
    return () => {
      active = false;
    };
  }, [config.endpoint]);

  const label =
    account?.label ||
    (settled ? translate("shell.hostedAccess") : translate("shell.loadingAccount"));
  const detail =
    account?.detail ||
    (settled ? translate("shell.accountUnavailable") : translate("shell.managedByHost"));
  const itemClass =
    "flex cursor-pointer select-none items-center gap-2 rounded px-2 py-2 text-sm text-foreground no-underline outline-none data-[highlighted]:bg-muted";

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
          aria-label={translate("shell.hostedAccountMenu", { label })}
          disabled={!settled}
        >
          <CircleUserRound
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
            <span className="block truncate text-sm font-medium leading-tight">{label}</span>
            <span
              className={cn(
                "block truncate text-xs leading-tight",
                placement === "sidebar" ? "text-shell-muted-foreground" : "text-muted-foreground",
              )}
            >
              {detail}
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
          className="z-[100] min-w-64 rounded-md border border-border bg-surface p-1 text-foreground shadow-lg"
        >
          <DropdownMenuPrimitive.Label className="px-2 py-2">
            <span className="block text-sm font-medium">{label}</span>
            <span className="block text-xs text-muted-foreground">{detail}</span>
          </DropdownMenuPrimitive.Label>
          {account?.accountURL || account?.logoutURL ? (
            <DropdownMenuPrimitive.Separator className="my-1 h-px bg-border" />
          ) : null}
          {account?.accountURL ? (
            <DropdownMenuPrimitive.Item asChild>
              <a className={itemClass} href={account.accountURL}>
                <Settings size={16} />
                {account.accountLabel}
              </a>
            </DropdownMenuPrimitive.Item>
          ) : null}
          {account?.logoutURL ? (
            <DropdownMenuPrimitive.Item asChild>
              <a className={itemClass} href={account.logoutURL}>
                <LogOut size={16} />
                {account.logoutLabel}
              </a>
            </DropdownMenuPrimitive.Item>
          ) : null}
        </DropdownMenuPrimitive.Content>
      </DropdownMenuPrimitive.Portal>
    </DropdownMenuPrimitive.Root>
  );
}

function AccountContext({
  hostAccount,
  placement = "topbar",
  collapsed = false,
}: {
  hostAccount: HostAccountConfig | null;
  placement?: "topbar" | "sidebar";
  collapsed?: boolean;
}) {
  return hostAccount ? (
    <HostedAccountMenu config={hostAccount} placement={placement} collapsed={collapsed} />
  ) : (
    <UserMenu placement={placement} collapsed={collapsed} />
  );
}

function MobileNavigation({
  path,
  hostAccount,
}: {
  path: string;
  hostAccount: HostAccountConfig | null;
}) {
  const [open, setOpen] = useState(false);

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Trigger asChild>
        <button
          className="mobileNavTrigger icon-control"
          type="button"
          aria-label={translate("navigation.open")}
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
                  {translate("shell.productSubtitle")}
                </span>
              </span>
            </DialogPrimitive.Title>
            <DialogPrimitive.Close
              className="icon-control"
              aria-label={translate("navigation.close")}
            >
              <X size={17} aria-hidden="true" />
            </DialogPrimitive.Close>
          </header>
          <nav
            className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-4"
            aria-label={translate("navigation.mobile")}
          >
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
                  <span>{translate(item.labelKey)}</span>
                </Link>
              );
            })}
          </nav>
          <footer className="grid gap-2 border-t border-shell-border p-3">
            <AccountContext hostAccount={hostAccount} placement="sidebar" />
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
  const { toasts, dismissToast, runtimeConfig, localAccessOpen, dismissLocalAccess } = useApp();
  const [collapsed, setCollapsed] = useState(loadCollapsed);

  useEffect(() => {
    globalThis.localStorage?.setItem("wf.sidebarCollapsed", String(collapsed));
  }, [collapsed]);

  if (scope === "instance") {
    const isRegistry = path === "/workspaces";
    return (
      <div className="min-h-screen bg-background text-foreground">
        <header className="sticky top-0 z-30 flex h-[var(--shell-header-height)] items-center justify-between border-b border-border bg-background px-4 sm:px-6">
          <nav
            className="instanceBreadcrumb flex min-w-0 items-center gap-2 text-sm"
            aria-label={translate("navigation.breadcrumb")}
          >
            <Link
              className="flex shrink-0 items-center gap-2 font-semibold text-foreground no-underline"
              to="/"
            >
              <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                <Wind size={16} strokeWidth={2.2} />
              </span>
              <span className="hidden sm:inline">Windforce</span>
            </Link>
            <span className="text-muted-foreground" aria-hidden="true">
              /
            </span>
            <span className="text-muted-foreground">{translate("navigation.instance")}</span>
            <span className="text-muted-foreground" aria-hidden="true">
              /
            </span>
            {isRegistry ? (
              <span className="truncate font-medium text-foreground">
                {translate("navigation.workspaces")}
              </span>
            ) : (
              <>
                <Link
                  className="text-muted-foreground no-underline hover:text-foreground"
                  to="/workspaces"
                >
                  {translate("navigation.workspaces")}
                </Link>
                <span className="text-muted-foreground" aria-hidden="true">
                  /
                </span>
                <span className="truncate font-medium text-foreground">{title}</span>
              </>
            )}
          </nav>
          <div className="flex shrink-0 items-center gap-2">
            <LocaleSwitcher />
            <ThemeToggle />
            <AccountContext hostAccount={runtimeConfig?.hostAccount || null} />
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
        {localAccessOpen && runtimeConfig?.authMode === "browser_token" ? (
          <LocalBrowserAccessDialog onClose={dismissLocalAccess} />
        ) : null}
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
                  {translate("shell.productSubtitle")}
                </span>
              </span>
            ) : null}
          </Link>
        </div>
        <nav
          className="flex flex-1 flex-col gap-1 overflow-y-auto px-3 py-4"
          aria-label={translate("navigation.primary")}
        >
          {primaryNavItems.map((item) => {
            const Icon = item.icon;
            const active = item.match(path);
            return (
              <Link
                key={item.to}
                to={item.to}
                className={cn("navItem", active && "active", collapsed && "justify-center px-0")}
                title={translate(item.labelKey)}
              >
                <Icon size={17} strokeWidth={1.9} aria-hidden="true" />
                {!collapsed ? <span>{translate(item.labelKey)}</span> : null}
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
          <AccountContext
            hostAccount={runtimeConfig?.hostAccount || null}
            placement="sidebar"
            collapsed={collapsed}
          />
        </footer>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-[var(--shell-header-height)] shrink-0 items-center justify-between gap-3 border-b border-border bg-background px-4 sm:px-6">
          <div className="flex min-w-0 items-center gap-2">
            <MobileNavigation path={path} hostAccount={runtimeConfig?.hostAccount || null} />
            <button
              className="icon-control sidebarCollapseControl"
              id="sidebarToggle"
              type="button"
              aria-label={
                collapsed
                  ? translate("navigation.expandSidebar")
                  : translate("navigation.collapseSidebar")
              }
              aria-expanded={!collapsed}
              title={
                collapsed
                  ? translate("navigation.expandSidebar")
                  : translate("navigation.collapseSidebar")
              }
              onClick={() => setCollapsed((current) => !current)}
            >
              {collapsed ? <PanelLeftOpen size={17} /> : <PanelLeftClose size={17} />}
            </button>
            <div
              className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground"
              data-testid="workspace-topbar-context"
            >
              <WorkspaceSwitcher variant="breadcrumb" />
              <span aria-hidden="true">/</span>
              <span className="truncate font-medium text-foreground">{title}</span>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <HostConsoleAction hostConsole={runtimeConfig?.hostConsole || null} />
            <LocaleSwitcher />
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
      {localAccessOpen && runtimeConfig?.authMode === "browser_token" ? (
        <LocalBrowserAccessDialog onClose={dismissLocalAccess} />
      ) : null}
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
            aria-label={translate("common.dismiss")}
            onClick={() => dismissToast(toast.id)}
          >
            <X size={15} />
          </button>
        </div>
      ))}
    </div>
  );
}
