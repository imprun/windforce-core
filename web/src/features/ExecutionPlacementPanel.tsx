import { AlertTriangle, Server, Tags } from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { ErrorNotice, Field, Modal, Panel, SelectControl } from "../components/ui";
import {
  type ActionView,
  type AppDetail,
  errorMessage,
  type RoutingPolicyPatch,
  type WorkerView,
} from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { translate } from "../shared/i18n";

type RoutingTarget = { kind: "app" } | { kind: "action"; action: ActionView };

export function ExecutionPlacementPanel({
  detail,
  onUpdated,
}: {
  detail: AppDetail;
  onUpdated: () => void;
}) {
  const { api } = useApp();
  const [target, setTarget] = useState<RoutingTarget | null>(null);
  const workers = useAsync(() => api.workers(), [api]);
  const app = detail.app;

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
        {workers.error ? <ErrorNotice message={workers.error} onRetry={workers.reload} /> : null}
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

        <div className="tableWrap routingActionTable">
          <table className="table">
            <thead>
              <tr>
                <th>{translate("trigger.column.action")}</th>
                <th>{translate("routing.effectiveRoute")}</th>
                <th>{translate("routing.effectiveLabels")}</th>
                <th>{translate("routing.workerAvailability")}</th>
                <th aria-label={translate("common.actions")} />
              </tr>
            </thead>
            <tbody>
              {detail.actions.map((action) => {
                const routeTag = action.effective_route_tag || app.effective_route_tag || app.tag;
                const labels = action.effective_required_labels || [];
                const matchingWorkers = countMatchingWorkers(
                  workers.data?.workers || [],
                  routeTag,
                  labels,
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
                      {workers.loading ? (
                        <span className="cellSub">{translate("common.loading")}</span>
                      ) : matchingWorkers > 0 ? (
                        <span className="badge badge-good">
                          {translate("routing.workersReady", { count: matchingWorkers })}
                        </span>
                      ) : (
                        <span className="badge badge-warning">
                          <AlertTriangle size={13} aria-hidden="true" />
                          {translate("routing.noReadyWorker")}
                        </span>
                      )}
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

export function countMatchingWorkers(
  workers: WorkerView[],
  routeTag: string,
  labels: string[],
): number {
  return workers.filter((worker) => {
    if (!worker.live || (worker.tags.length > 0 && !worker.tags.includes(routeTag))) return false;
    return labels.every((label) => worker.labels.includes(label));
  }).length;
}
