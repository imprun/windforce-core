import { AlertTriangle, Server, Tags } from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { ErrorNotice, Field, Modal, Panel, SelectControl } from "../components/ui";
import {
  type ActionView,
  type AppDetail,
  type ExecutionDemand,
  type ExecutionDemandTarget,
  errorMessage,
  type PlacementCandidates,
  type PlacementReasonCode,
  type PlacementTargetCandidates,
  type RoutingPolicyPatch,
} from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative } from "../lib/format";
import { type TranslationKey, translate } from "../shared/i18n";

type RoutingTarget = { kind: "app" } | { kind: "action"; action: ActionView };

const placementReasonKeys: Record<PlacementReasonCode, TranslationKey> = {
  workspace_not_allowed: "routing.reason.workspace_not_allowed",
  draining: "routing.reason.draining",
  no_live_capacity: "routing.reason.no_live_capacity",
  missing_tag: "routing.reason.missing_tag",
  missing_label: "routing.reason.missing_label",
  execution_profile_mismatch: "routing.reason.execution_profile_mismatch",
  resource_pressure: "routing.reason.resource_pressure",
};

export function ExecutionPlacementPanel({
  detail,
  onUpdated,
}: {
  detail: AppDetail;
  onUpdated: () => void;
}) {
  const { api } = useApp();
  const [target, setTarget] = useState<RoutingTarget | null>(null);
  const app = detail.app;
  const placement = useAsync(() => api.placementCandidates(app.app_key), [api, app.app_key]);
  const demand = useAsync(() => api.executionDemand(app.app_key), [api, app.app_key]);
  const appPlacement = placementTargetForAction(placement.data, undefined);
  const appDemand = summarizeDemandTargets(demand.data?.targets || []);

  return (
    <>
      <Panel
        title={translate("routing.title")}
        subtitle={translate("routing.subtitle")}
        actions={
          <button
            className="button"
            type="button"
            data-ui-guide="edit-app-execution-placement"
            onClick={() => setTarget({ kind: "app" })}
          >
            {translate("routing.editApp")}
          </button>
        }
      >
        {placement.error ? (
          <ErrorNotice message={placement.error} onRetry={placement.reload} />
        ) : null}
        {demand.error ? <ErrorNotice message={demand.error} onRetry={demand.reload} /> : null}
        <div className="routingPolicySummary">
          <RoutingValue
            icon={<Server size={16} aria-hidden="true" />}
            label={translate("routing.routeTag")}
            manifest={app.tag || "default"}
            override={app.tag_override}
            effective={app.effective_route_tag || app.tag || "default"}
          />
          <RoutingValue
            icon={<Tags size={16} aria-hidden="true" />}
            label={translate("routing.requiredLabels")}
            manifest={formatLabels(app.required_labels)}
            override={formatOverrideLabels(app.required_labels_override)}
            effective={formatLabels(app.effective_required_labels)}
          />
        </div>

        <div className="mt-4 rounded-lg border border-border bg-muted/30 p-4">
          <div className="mb-2 text-sm font-medium">{translate("routing.appCapacity")}</div>
          <PlacementAvailability target={appPlacement} loading={placement.loading} />
          <div className="mt-3 border-t border-border pt-3">
            <ExecutionDemandAvailability summary={appDemand} loading={demand.loading} />
          </div>
        </div>

        <div className="tableWrap routingActionTable">
          <table className="table">
            <thead>
              <tr>
                <th>{translate("trigger.column.action")}</th>
                <th>{translate("routing.effectiveRoute")}</th>
                <th>{translate("routing.effectiveLabels")}</th>
                <th>{translate("routing.workerAvailability")}</th>
                <th>{translate("routing.executionDemand")}</th>
                <th aria-label={translate("common.actions")} />
              </tr>
            </thead>
            <tbody>
              {detail.actions.map((action) => {
                const routeTag = action.effective_route_tag || app.effective_route_tag || app.tag;
                const labels = action.effective_required_labels || [];
                const actionPlacement = placementTargetForAction(placement.data, action.action_key);
                const actionDemand = summarizeDemandTargets(
                  executionDemandTargetsForAction(demand.data, action.action_key),
                );
                return (
                  <tr key={action.action_key}>
                    <td>
                      <span className="cellTitle">{action.display_name || action.action_key}</span>
                      <span className="cellSub mono">{action.action_key}</span>
                    </td>
                    <td>
                      <span className="mono">{routeTag}</span>
                    </td>
                    <td>{formatLabels(labels)}</td>
                    <td>
                      <PlacementAvailability target={actionPlacement} loading={placement.loading} />
                    </td>
                    <td>
                      <ExecutionDemandAvailability
                        summary={actionDemand}
                        loading={demand.loading}
                      />
                    </td>
                    <td>
                      <button
                        className="button small"
                        type="button"
                        data-ui-guide={`edit-action-execution-placement-${action.action_key}`}
                        onClick={() => setTarget({ kind: "action", action })}
                      >
                        {translate("common.edit")}
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Panel>
      {target ? (
        <RoutingPolicyDialog
          app={detail.app}
          target={target}
          onClose={() => setTarget(null)}
          onSaved={() => {
            setTarget(null);
            onUpdated();
          }}
        />
      ) : null}
    </>
  );
}

function PlacementAvailability({
  target,
  loading,
}: {
  target: PlacementTargetCandidates | undefined;
  loading: boolean;
}) {
  if (loading && !target) {
    return <span className="cellSub">{translate("common.loading")}</span>;
  }
  if (!target) {
    return (
      <span className="badge badge-warning">
        <AlertTriangle size={13} aria-hidden="true" />
        {translate("routing.capacityUnavailable")}
      </span>
    );
  }
  const eligible = target.candidates.filter((candidate) => candidate.eligible);
  const excluded = target.candidates.filter((candidate) => !candidate.eligible);
  const capacity = placementCapacity(target);
  return (
    <div className="grid min-w-56 gap-1.5">
      {capacity.totalSlots === 0 ? (
        <span className="badge badge-warning w-fit">
          <AlertTriangle size={13} aria-hidden="true" />
          {translate("routing.noReadyWorker")}
        </span>
      ) : capacity.saturated ? (
        <span className="badge badge-warning w-fit">
          <AlertTriangle size={13} aria-hidden="true" />
          {translate("routing.slotsSaturated", { total: capacity.totalSlots })}
        </span>
      ) : (
        <span className="badge badge-good w-fit">
          {translate("routing.slotsAvailable", {
            workers: target.matching_workers,
            available: capacity.availableSlots,
            total: capacity.totalSlots,
          })}
        </span>
      )}
      {eligible.length > 0 ? (
        <span className="cellSub">
          {translate("routing.eligibleGroups", {
            groups: eligible
              .map(
                (candidate) =>
                  `${candidate.group} (${candidate.available_slots}/${candidate.matching_slots})`,
              )
              .join(", "),
          })}
        </span>
      ) : null}
      {excluded.length > 0 ? (
        <details className="text-xs text-muted-foreground">
          <summary className="cursor-pointer">{translate("routing.excludedGroups")}</summary>
          <ul className="mt-1 grid gap-1 pl-4">
            {excluded.map((candidate) => (
              <li key={candidate.group}>
                <span className="mono">{candidate.group}</span>:{" "}
                {formatCandidateReasons(candidate.reason_codes)}
              </li>
            ))}
          </ul>
        </details>
      ) : null}
    </div>
  );
}

type DemandSummary = {
  queuedJobs: number;
  oldestQueuedAt?: string;
  targets: ExecutionDemandTarget[];
};

function ExecutionDemandAvailability({
  summary,
  loading,
}: {
  summary: DemandSummary;
  loading: boolean;
}) {
  if (loading && summary.targets.length === 0) {
    return <span className="cellSub">{translate("common.loading")}</span>;
  }
  if (summary.queuedJobs === 0) {
    return <span className="badge badge-neutral w-fit">{translate("routing.noQueuedRuns")}</span>;
  }
  return (
    <div className="grid min-w-56 gap-1.5">
      <span className="badge badge-warning w-fit">
        {translate("routing.queuedRuns", { count: summary.queuedJobs })}
      </span>
      <span className="cellSub">
        {translate("routing.oldestQueued", { time: formatRelative(summary.oldestQueuedAt) })}
      </span>
      {summary.targets.length === 1 ? (
        <PinnedTargetCapacity target={summary.targets[0]!} />
      ) : (
        <details className="text-xs text-muted-foreground">
          <summary className="cursor-pointer">
            {translate("routing.pinnedTargets", { count: summary.targets.length })}
          </summary>
          <ul className="mt-1 grid gap-1 pl-4">
            {summary.targets.map((target) => (
              <li key={executionDemandTargetKey(target)}>
                <PinnedTargetCapacity target={target} />
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}

function PinnedTargetCapacity({ target }: { target: ExecutionDemandTarget }) {
  const selector = target.effective_required_labels.length
    ? `${target.effective_tag} · ${target.effective_required_labels.join(", ")}`
    : target.effective_tag;
  const capacity =
    target.total_slots === 0
      ? translate("routing.pinnedNoCapacity")
      : target.saturated
        ? translate("routing.pinnedSaturated", { total: target.total_slots })
        : translate("routing.pinnedAvailable", {
            available: target.available_slots,
            total: target.total_slots,
          });
  return (
    <span className="cellSub">
      <span className="mono">{selector}</span>: {capacity}
    </span>
  );
}

function RoutingValue({
  icon,
  label,
  manifest,
  override,
  effective,
}: {
  icon: ReactNode;
  label: string;
  manifest: string;
  override: string | undefined;
  effective: string;
}) {
  return (
    <section className="routingValue">
      <h3>
        {icon}
        {label}
      </h3>
      <dl>
        <div>
          <dt>{translate("routing.releaseDefault")}</dt>
          <dd>{manifest}</dd>
        </div>
        <div>
          <dt>{translate("routing.operatorOverride")}</dt>
          <dd>{override ?? translate("routing.inherit")}</dd>
        </div>
        <div className="routingEffective">
          <dt>{translate("routing.effective")}</dt>
          <dd>{effective}</dd>
        </div>
      </dl>
    </section>
  );
}

function RoutingPolicyDialog({
  app,
  target,
  onClose,
  onSaved,
}: {
  app: AppDetail["app"];
  target: RoutingTarget;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api, notify } = useApp();
  const action = target.kind === "action" ? target.action : null;
  const initialTagOverride = action ? action.tag_override : app.tag_override;
  const initialLabelsOverride = action
    ? action.required_labels_override
    : app.required_labels_override;
  const [tagMode, setTagMode] = useState(initialTagOverride === undefined ? "inherit" : "override");
  const [tagOverride, setTagOverride] = useState(initialTagOverride || "");
  const [labelMode, setLabelMode] = useState(
    initialLabelsOverride === undefined ? "inherit" : "override",
  );
  const [labelText, setLabelText] = useState((initialLabelsOverride || []).join(", "));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const labels = useMemo(() => parseLabels(labelText), [labelText]);
  const manifestTag = action?.tag || app.tag || "default";
  const inheritedTag = action ? app.tag_override || action.tag || app.tag : app.tag;
  const effectiveTag = tagMode === "override" ? tagOverride.trim() || inheritedTag : inheritedTag;
  const manifestLabels = action
    ? actionReleaseLabels(app.required_labels, action.required_labels)
    : app.required_labels || [];
  const inheritedLabels = action
    ? (app.required_labels_override ?? manifestLabels)
    : app.required_labels || [];
  const effectiveLabels = labelMode === "override" ? labels : inheritedLabels;

  async function save() {
    setBusy(true);
    setError("");
    const patch: RoutingPolicyPatch = {
      tag_override: tagMode === "inherit" ? null : tagOverride.trim(),
      required_labels_override: labelMode === "inherit" ? null : labels,
    };
    try {
      if (action) {
        await api.patchActionRoutingPolicy(app.app_key, action.action_key, patch);
      } else {
        await api.patchAppRoutingPolicy(app.app_key, patch);
      }
      notify("ok", translate("routing.saved"));
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={
        action
          ? translate("routing.editActionNamed", { action: action.action_key })
          : translate("routing.editApp")
      }
      subtitle={translate("routing.dialogHint")}
      onClose={onClose}
      wide
    >
      <div className="routingEditorGrid">
        <section className="routingEditorField">
          <Field label={translate("routing.routeTag")} hint={translate("routing.routeTagHint")}>
            <SelectControl
              value={tagMode}
              onChange={setTagMode}
              ariaLabel={translate("routing.routeTagMode")}
              options={[
                {
                  value: "inherit",
                  label: translate(
                    action ? "routing.inheritAppPlacement" : "routing.inheritRelease",
                  ),
                },
                { value: "override", label: translate("routing.override") },
              ]}
            />
          </Field>
          {tagMode === "override" ? (
            <Field label={translate("routing.overrideValue")}>
              <input value={tagOverride} onChange={(event) => setTagOverride(event.target.value)} />
            </Field>
          ) : null}
          <RoutingPreview manifest={manifestTag} effective={effectiveTag} />
        </section>

        <section className="routingEditorField">
          <Field
            label={translate("routing.requiredLabels")}
            hint={translate("routing.requiredLabelsHint")}
          >
            <SelectControl
              value={labelMode}
              onChange={setLabelMode}
              ariaLabel={translate("routing.requiredLabelsMode")}
              options={[
                {
                  value: "inherit",
                  label: translate(
                    action ? "routing.inheritAppPlacement" : "routing.inheritRelease",
                  ),
                },
                { value: "override", label: translate("routing.override") },
              ]}
            />
          </Field>
          {labelMode === "override" ? (
            <Field
              label={translate("routing.overrideValue")}
              hint={translate("routing.emptyLabelsHint")}
            >
              <input
                value={labelText}
                onChange={(event) => setLabelText(event.target.value)}
                placeholder="linux, browser"
              />
            </Field>
          ) : null}
          <RoutingPreview
            manifest={formatLabels(manifestLabels)}
            effective={formatLabels(effectiveLabels)}
          />
        </section>
      </div>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter dialogFooterEnd">
        <button className="button" type="button" disabled={busy} onClick={onClose}>
          {translate("common.cancel")}
        </button>
        <button
          className="button primary"
          type="button"
          disabled={busy || (tagMode === "override" && !tagOverride.trim())}
          onClick={save}
        >
          {busy ? translate("common.saving") : translate("common.saveChanges")}
        </button>
      </footer>
    </Modal>
  );
}

function RoutingPreview({ manifest, effective }: { manifest: string; effective: string }) {
  return (
    <dl className="routingPreview">
      <div>
        <dt>{translate("routing.releaseDefault")}</dt>
        <dd>{manifest}</dd>
      </div>
      <div>
        <dt>{translate("routing.effectiveAfterSave")}</dt>
        <dd>{effective}</dd>
      </div>
    </dl>
  );
}

function parseLabels(value: string): string[] {
  return uniqueLabels(
    value
      .split(/[\n,]/)
      .map((item) => item.trim())
      .filter(Boolean),
  );
}

function uniqueLabels(labels: string[]): string[] {
  return [...new Set(labels)].sort();
}

function formatLabels(labels?: string[]): string {
  return labels?.length ? labels.join(", ") : translate("routing.noLabels");
}

function formatOverrideLabels(labels?: string[]): string | undefined {
  return labels === undefined ? undefined : formatLabels(labels);
}

export function actionReleaseLabels(appLabels?: string[], actionLabels?: string[]): string[] {
  return uniqueLabels([...(appLabels || []), ...(actionLabels || [])]);
}

export function placementTargetForAction(
  placement: PlacementCandidates | null | undefined,
  actionKey: string | undefined,
): PlacementTargetCandidates | undefined {
  return placement?.targets.find((target) =>
    actionKey === undefined ? !target.action : target.action === actionKey,
  );
}

export function placementCapacity(target: PlacementTargetCandidates) {
  const eligible = target.candidates.filter((candidate) => candidate.eligible);
  const occupiedSlots = eligible.reduce((total, candidate) => total + candidate.occupied_slots, 0);
  const availableSlots = eligible.reduce(
    (total, candidate) => total + candidate.available_slots,
    0,
  );
  return {
    totalSlots: target.matching_slots,
    occupiedSlots,
    availableSlots,
    saturated: target.matching_slots > 0 && availableSlots === 0,
  };
}

export function executionDemandTargetsForAction(
  demand: ExecutionDemand | null | undefined,
  actionKey: string,
): ExecutionDemandTarget[] {
  return demand?.targets.filter((target) => target.action === actionKey) || [];
}

export function summarizeDemandTargets(targets: ExecutionDemandTarget[]): DemandSummary {
  let oldestQueuedAt: string | undefined;
  let queuedJobs = 0;
  for (const target of targets) {
    queuedJobs += target.queued_jobs;
    if (
      target.oldest_queued_at &&
      (!oldestQueuedAt || Date.parse(target.oldest_queued_at) < Date.parse(oldestQueuedAt))
    ) {
      oldestQueuedAt = target.oldest_queued_at;
    }
  }
  return { queuedJobs, oldestQueuedAt, targets };
}

function executionDemandTargetKey(target: ExecutionDemandTarget): string {
  return [
    target.app,
    target.action,
    target.effective_tag,
    target.effective_required_labels.join("\u001f"),
    target.execution_profile.key,
  ].join("\u001e");
}

function formatCandidateReasons(reasons: PlacementReasonCode[]): string {
  if (reasons.length === 0) return translate("routing.reason.unknown");
  return reasons.map((reason) => translate(placementReasonKeys[reason])).join(", ");
}
