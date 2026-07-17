export type ReleaseActionState = {
  syncLabel: "Sync source" | "Source current";
  syncDisabled: boolean;
  publishLabel: "Sync required" | "Up to date" | "Publish Release" | "Republish required";
  publishDisabled: boolean;
};

export function releaseActionState(
  activeCommit: string | null | undefined,
  latestSyncedCommit: string | null | undefined,
  sourceChecked: boolean,
  activeBundleReady: boolean,
): ReleaseActionState {
  const active = activeCommit?.trim() || "";
  const latest = latestSyncedCommit?.trim() || "";

  let publishLabel: ReleaseActionState["publishLabel"] = "Publish Release";
  let publishDisabled = false;
  if (!latest) {
    publishLabel = "Sync required";
    publishDisabled = true;
  } else if (active === latest) {
    publishLabel = activeBundleReady ? "Up to date" : "Republish required";
    publishDisabled = activeBundleReady;
  }

  return {
    syncLabel: sourceChecked ? "Source current" : "Sync source",
    syncDisabled: sourceChecked,
    publishLabel,
    publishDisabled,
  };
}
