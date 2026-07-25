import { X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import type { ReactNode } from "react";
import type { ProbeResult } from "../lib/api";
import { formatJSON } from "../lib/format";

export function ReleaseStateBadge({
  released,
  bundleReady = true,
}: {
  released: boolean;
  bundleReady?: boolean;
}) {
  if (released && !bundleReady) {
    return (
      <span className="badge badge-warning">
        <span aria-hidden="true" className="badgeIcon">
          !
        </span>
        repair required
      </span>
    );
  }
  return released ? (
    <span className="badge badge-good">
      <span aria-hidden="true" className="badgeIcon">
        ✓
      </span>
      released
    </span>
  ) : (
    <span className="badge badge-neutral">
      <span aria-hidden="true" className="badgeIcon">
        ○
      </span>
      registered
    </span>
  );
}

export function Panel({
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
  return (
    <section className="panel">
      <header className="panelHeader">
        <div>
          <h2>{title}</h2>
          {subtitle ? <p className="panelSubtitle">{subtitle}</p> : null}
        </div>
        {actions ? <div className="panelActions">{actions}</div> : null}
      </header>
      <div className="panelBody">{children}</div>
    </section>
  );
}

export function DefinitionList({
  items,
  className,
}: {
  items: Array<[string, ReactNode]>;
  className?: string;
}) {
  return (
    <dl className={className ? `defList ${className}` : "defList"}>
      {items.map(([label, value]) => (
        <div className="defItem" key={label}>
          <dt>{label}</dt>
          <dd>{value ?? "—"}</dd>
        </div>
      ))}
    </dl>
  );
}

export function ImpMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={className} aria-hidden="true">
      <path
        d="M6 9c0-2 .6-4.4-1-6 2.6 0 4.5 1 5.5 2.4h3C14.5 4 16.4 3 19 3c-1.6 1.6-1 4-1 6a6 6 0 0 1-2 4.6V17a4 4 0 0 1-8 0v-3.4A6 6 0 0 1 6 9Z"
        fill="currentColor"
      />
      <circle cx="9.8" cy="10" r="1.1" fill="var(--surface)" />
      <circle cx="14.2" cy="10" r="1.1" fill="var(--surface)" />
    </svg>
  );
}

export function EmptyState({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="emptyState">
      <span className="emptyMark">
        <ImpMark />
      </span>
      <p className="emptyTitle">{title}</p>
      {children ? <div className="emptyBody">{children}</div> : null}
    </div>
  );
}

export function ErrorNotice({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="errorNotice" role="alert">
      <span>{message}</span>
      {onRetry ? (
        <button className="button small" type="button" onClick={onRetry}>
          Retry
        </button>
      ) : null}
    </div>
  );
}

export function Loading({ label = "Loading…" }: { label?: string }) {
  return <p className="loading">{label}</p>;
}

export function JsonBlock({ value, maxHeight }: { value: unknown; maxHeight?: number }) {
  const text = typeof value === "string" ? value : formatJSON(value);
  return (
    <pre className="codeBlock" style={maxHeight ? { maxHeight } : undefined}>
      {text || "(empty)"}
    </pre>
  );
}

export function Modal({
  title,
  subtitle,
  onClose,
  children,
  id,
  wide,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: ReactNode;
  id?: string;
  wide?: boolean;
}) {
  return (
    <DialogPrimitive.Root open onOpenChange={(open) => !open && onClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="modalBackdrop" />
        <DialogPrimitive.Content className={wide ? "dialog wide" : "dialog"} id={id}>
          <header className="dialogHeader">
            <div>
              <DialogPrimitive.Title>{title}</DialogPrimitive.Title>
              {subtitle ? (
                <DialogPrimitive.Description>{subtitle}</DialogPrimitive.Description>
              ) : null}
            </div>
            <DialogPrimitive.Close className="icon-control" aria-label="Close" title="Close">
              <X size={18} aria-hidden="true" />
            </DialogPrimitive.Close>
          </header>
          <div className="dialogBody">{children}</div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function Sheet({
  title,
  subtitle,
  onClose,
  children,
  actions,
  id,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: ReactNode;
  actions?: ReactNode;
  id?: string;
}) {
  return (
    <DialogPrimitive.Root open onOpenChange={(open) => !open && onClose()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="sheetBackdrop" />
        <DialogPrimitive.Content className="sheet" id={id}>
          <header className="sheetHeader">
            <div>
              <DialogPrimitive.Title>{title}</DialogPrimitive.Title>
              {subtitle ? (
                <DialogPrimitive.Description>{subtitle}</DialogPrimitive.Description>
              ) : null}
            </div>
            <DialogPrimitive.Close className="icon-control" aria-label="Close" title="Close">
              <X size={18} aria-hidden="true" />
            </DialogPrimitive.Close>
          </header>
          <div className="sheetBody">{children}</div>
          {actions ? <footer className="sheetFooter">{actions}</footer> : null}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function ProbeNotice({ probe, branch }: { probe: ProbeResult; branch: string }) {
  if (!probe.reachable) {
    return (
      <div className="inlineNotice error">{probe.error || "Repository is not reachable."}</div>
    );
  }
  const branchName = probe.branch || branch;
  const branches = probe.branches?.length
    ? ` Remote branches: ${probe.branches.slice(0, 8).join(", ")}.`
    : "";
  return (
    <div className="inlineNotice ok">
      Repository reachable. Branch {branchName} {probe.branch_exists ? "exists" : "was not found"}.
      {branches}
    </div>
  );
}

export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="field">
      <span className="fieldLabel">{label}</span>
      {children}
      {hint ? <span className="fieldHint">{hint}</span> : null}
    </label>
  );
}
