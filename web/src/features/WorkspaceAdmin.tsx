import { Check, Copy } from "lucide-react";
import { useState } from "react";
import type { Workspace } from "../lib/api";
import { useApp } from "../lib/app-context";
import { useRouter } from "../lib/router";
import { translate } from "../shared/i18n";

export function WorkspaceStatus({ workspace }: { workspace: Workspace }) {
  return workspace.status === "active" ? (
    <span className="badge badge-good">{translate("workspace.status.active")}</span>
  ) : (
    <span className="badge badge-neutral">{translate("common.archived")}</span>
  );
}

export function WorkspaceActivation({
  workspace,
  compact = false,
}: {
  workspace: Workspace;
  compact?: boolean;
}) {
  const { settings, updateSettings } = useApp();
  const { navigate } = useRouter();
  const current = workspace.id === settings.workspace;

  if (current) {
    return (
      <span className="badge badge-current">
        <Check size={13} aria-hidden="true" /> {translate("workspace.currentBadge")}
      </span>
    );
  }

  return (
    <button
      className={compact ? "button small primary" : "button primary"}
      type="button"
      disabled={workspace.status === "archived"}
      title={
        workspace.status === "archived"
          ? translate("workspace.archivedCannotSelect")
          : translate("workspace.switchToNamed", { name: workspace.name })
      }
      onClick={() => {
        updateSettings({ ...settings, workspace: workspace.id });
        navigate("/");
      }}
    >
      {compact ? translate("workspace.switchShort") : translate("workspace.switchTo")}
    </button>
  );
}

export function OneTimeWorkspaceToken({ token }: { token: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="oneTimeToken">
      <p className="fieldLabel">{translate("workspace.oneTimeToken")}</p>
      <div className="copyField">
        <code>{token}</code>
        <button
          className="button small"
          type="button"
          title={translate("workspace.copyToken")}
          aria-label={translate("workspace.copyToken")}
          onClick={async () => {
            await navigator.clipboard.writeText(token);
            setCopied(true);
          }}
        >
          {copied ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
          {copied ? translate("common.copied") : translate("workspace.copyToken")}
        </button>
      </div>
      <p className="fieldHint">{translate("workspace.oneTimeTokenHint")}</p>
    </div>
  );
}
