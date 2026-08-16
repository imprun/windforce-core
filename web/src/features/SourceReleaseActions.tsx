import { Check, RefreshCw, Rocket } from "lucide-react";
import { useState } from "react";
import type { GitSource, SourceSyncResult } from "../lib/api";
import { useApp } from "../lib/app-context";
import { shortSHA } from "../lib/format";
import { releaseActionState } from "../lib/release-actions";
import { sourceErrorMessage } from "../lib/repository-settings";
import { translate } from "../shared/i18n";

export function SourceReleaseActions({
  source,
  activeCommit,
  activeBundleReady,
  compact = false,
  syncButtonID,
  publishButtonID,
  onSynced,
  onPublish,
}: {
  source: GitSource;
  activeCommit?: string;
  activeBundleReady: boolean;
  compact?: boolean;
  syncButtonID?: string;
  publishButtonID?: string;
  onSynced?: (result: SourceSyncResult) => void;
  onPublish: (source: GitSource) => void;
}) {
  const { api, notify } = useApp();
  const [syncing, setSyncing] = useState(false);
  const [syncResult, setSyncResult] = useState<SourceSyncResult | null>(null);
  const latestCommit = syncResult?.commit || source.last_synced_commit || "";
  const state = releaseActionState(
    activeCommit,
    latestCommit,
    Boolean(syncResult),
    activeBundleReady,
  );
  const buttonClass = compact ? "button small" : "button";

  async function syncSource() {
    setSyncing(true);
    try {
      const result = await api.syncGitSource(source.id);
      setSyncResult(result);
      if (result.commit === source.last_synced_commit) {
        notify(
          "ok",
          translate("release.alreadySynchronized", {
            app: result.app,
            commit: shortSHA(result.commit, 12),
          }),
        );
      } else {
        notify(
          "ok",
          translate("release.synchronized", {
            app: result.app,
            commit: shortSHA(result.commit, 12),
          }),
        );
      }
      onSynced?.(result);
    } catch (cause) {
      notify("error", sourceErrorMessage(cause));
    } finally {
      setSyncing(false);
    }
  }

  const effectiveSource: GitSource = syncResult
    ? { ...source, last_synced_commit: syncResult.commit, last_synced_at: syncResult.synced_at }
    : source;

  return (
    <div className="releaseActionGroup">
      <button
        className={buttonClass}
        id={syncButtonID}
        data-checked={syncResult ? "true" : "false"}
        type="button"
        disabled={syncing || state.syncDisabled}
        title={
          state.syncDisabled ? translate("release.branchChecked") : translate("release.checkBranch")
        }
        onClick={syncSource}
      >
        {syncing ? (
          <RefreshCw aria-hidden="true" className="spin" />
        ) : state.syncDisabled ? (
          <Check aria-hidden="true" />
        ) : (
          <RefreshCw aria-hidden="true" />
        )}
        {syncing ? translate("release.synchronizing") : syncActionLabel(state.syncLabel)}
      </button>
      <button
        className={`${buttonClass} primary`}
        id={publishButtonID}
        type="button"
        disabled={syncing || state.publishDisabled}
        title={publishButtonTitle(state.publishLabel)}
        onClick={() => onPublish(effectiveSource)}
      >
        <Rocket aria-hidden="true" />
        {publishActionLabel(state.publishLabel)}
      </button>
    </div>
  );
}

function publishButtonTitle(
  label: "Sync required" | "Up to date" | "Publish Release" | "Republish required",
): string {
  if (label === "Sync required") return translate("release.syncBeforePublishing");
  if (label === "Up to date") return translate("release.upToDateHint");
  if (label === "Republish required") return translate("release.republishHint");
  return translate("release.publishHint");
}

function syncActionLabel(label: "Sync source" | "Source current"): string {
  return label === "Sync source"
    ? translate("release.syncSource")
    : translate("release.sourceCurrent");
}

function publishActionLabel(
  label: "Sync required" | "Up to date" | "Publish Release" | "Republish required",
): string {
  if (label === "Sync required") return translate("release.syncRequired");
  if (label === "Up to date") return translate("release.upToDate");
  if (label === "Republish required") return translate("release.republishRequired");
  return translate("release.publish");
}
