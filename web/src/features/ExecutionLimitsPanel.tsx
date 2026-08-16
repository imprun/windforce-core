import { useEffect, useMemo, useState } from "react";
import { ErrorNotice, Loading, Panel } from "../components/ui";
import { actionDisplayName } from "../lib/action-label";
import {
  type ActionView,
  type AppDetail,
  type EnforcedExecutionLimit,
  type ExecutionLimitPolicy,
  type ExecutionLimitPolicyReadback,
  type ExecutionLimitResidual,
  type ExecutionLimits,
  errorMessage,
  type KeyedConcurrencyLimit,
  type KeyedRateLimit,
} from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { translate } from "../shared/i18n";

type ActionLimitRow = {
  actionKey: string;
  actionName: string;
} & ExecutionLimitRow;

export type ExecutionLimitRow =
  | { kind: "concurrency"; limit: KeyedConcurrencyLimit }
  | { kind: "rate"; limit: KeyedRateLimit };

export function ExecutionLimitsPanel({ detail }: { detail: AppDetail }) {
  const { api, notify } = useApp();
  const appKey = detail.app.app_key;
  const state = useAsync(() => api.executionLimitPolicies(appKey), [api, appKey]);
  const [allowances, setAllowances] = useState<Record<string, string>>({});
  const [busyKey, setBusyKey] = useState("");
  const [mutationError, setMutationError] = useState("");

  useEffect(() => {
    if (!state.data) return;
    const next: Record<string, string> = {};
    for (const shape of state.data.enforced.active_release) {
      const policy = activePolicyForShape(state.data, shape);
      next[executionLimitIdentity(shape)] =
        policy?.operator_allowance != null ? String(policy.operator_allowance) : "";
    }
    setAllowances(next);
  }, [state.data]);

  const actionNames = useMemo(
    () =>
      new Map(
        detail.actions.map((action) => [
          action.action_key,
          actionDisplayName(action.display_name) || action.action_key,
        ]),
      ),
    [detail.actions],
  );

  async function saveAllowance(shape: EnforcedExecutionLimit) {
    if (!state.data) return;
    const identity = executionLimitIdentity(shape);
    const allowance = Number(allowances[identity]);
    if (!Number.isSafeInteger(allowance) || allowance < 1 || allowance > 2_147_483_647) {
      setMutationError(translate("executionLimits.allowanceInvalid"));
      return;
    }
    const current = activePolicyForShape(state.data, shape);
    setBusyKey(identity);
    setMutationError("");
    try {
      await api.putExecutionLimitPolicy(appKey, {
        scope: shape.scope,
        action_key: shape.action_key,
        policy_id: shape.policy_id,
        kind: shape.kind,
        shape_fingerprint: shape.shape_fingerprint,
        allowance,
        window_seconds: shape.window_seconds,
        expected_revision: current?.revision || 0,
        operation_id: `execution_limit_${crypto.randomUUID()}`,
        reason: "Updated from the Core console",
      });
      notify("ok", translate("executionLimits.allowanceSaved"));
      state.reload();
    } catch (cause) {
      setMutationError(errorMessage(cause));
    } finally {
      setBusyKey("");
    }
  }

  async function removeAllowance(policy: ExecutionLimitPolicy) {
    const identity = executionLimitIdentity(policy);
    setBusyKey(identity);
    setMutationError("");
    try {
      await api.deleteExecutionLimitPolicy(appKey, {
        scope: policy.scope,
        action_key: policy.action_key,
        policy_id: policy.policy_id,
        kind: policy.kind,
        shape_fingerprint: policy.shape_fingerprint,
        window_seconds: policy.window_seconds,
        expected_revision: policy.revision,
        operation_id: `execution_limit_${crypto.randomUUID()}`,
        reason: "Returned to the Release ceiling from the Core console",
      });
      notify("ok", translate("executionLimits.allowanceRemoved"));
      state.reload();
    } catch (cause) {
      setMutationError(errorMessage(cause));
    } finally {
      setBusyKey("");
    }
  }

  return (
    <Panel
      title={translate("executionLimits.title")}
      subtitle={translate("executionLimits.subtitle")}
      actions={
        <button
          className="button small"
          type="button"
          disabled={state.loading}
          onClick={state.reload}
        >
          {translate("common.refresh")}
        </button>
      }
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading /> : null}
      {state.data ? (
        <ExecutionLimitPolicyContent
          readback={state.data}
          actionNames={actionNames}
          allowances={allowances}
          busyKey={busyKey}
          mutationError={mutationError}
          onAllowanceChange={(shape, value) =>
            setAllowances((current) => ({
              ...current,
              [executionLimitIdentity(shape)]: value,
            }))
          }
          onSave={saveAllowance}
          onRemove={removeAllowance}
        />
      ) : null}
    </Panel>
  );
}

function ExecutionLimitPolicyContent({
  readback,
  actionNames,
  allowances,
  busyKey,
  mutationError,
  onAllowanceChange,
  onSave,
  onRemove,
}: {
  readback: ExecutionLimitPolicyReadback;
  actionNames: Map<string, string>;
  allowances: Record<string, string>;
  busyKey: string;
  mutationError: string;
  onAllowanceChange: (shape: EnforcedExecutionLimit, value: string) => void;
  onSave: (shape: EnforcedExecutionLimit) => void;
  onRemove: (policy: ExecutionLimitPolicy) => void;
}) {
  const activePolicies = readback.desired.items.filter((policy) => policy.status === "applied");
  const dormantPolicies = readback.desired.items.filter((policy) => policy.status === "dormant");
  const queued = readback.enforced.residual_cohorts.reduce((sum, item) => sum + item.queued, 0);
  const running = readback.enforced.residual_cohorts.reduce((sum, item) => sum + item.running, 0);

  return (
    <div className="executionLimitPolicyView">
      <div className="executionLimitsIntro">
        <div className="executionLimitSummary" data-ui-guide="execution-limit-summary">
          <ExecutionLimitMetric
            label={translate("executionLimits.activeShapes")}
            value={readback.enforced.active_release.length}
          />
          <ExecutionLimitMetric
            label={translate("executionLimits.operatorPolicies")}
            value={activePolicies.length}
          />
          <ExecutionLimitMetric
            label={translate("executionLimits.pinnedJobs")}
            value={queued + running}
          />
        </div>
        <p>{translate("executionLimits.changeHint")}</p>
        <p className="fieldHint">
          {translate("executionLimits.observedRelease", {
            commit: shortFingerprint(readback.observed.commit_sha),
          })}
        </p>
      </div>

      {mutationError ? (
        <div className="inlineNotice error" role="alert">
          {mutationError}
        </div>
      ) : null}

      <section className="executionLimitSection" aria-labelledby="activeExecutionLimitsTitle">
        <header className="executionLimitSectionHeader">
          <div>
            <h3 id="activeExecutionLimitsTitle">{translate("executionLimits.activeTitle")}</h3>
            <p>{translate("executionLimits.activeHint")}</p>
          </div>
          <span className="badge badge-good">
            {translate("executionLimits.policyCount", {
              count: readback.enforced.active_release.length,
            })}
          </span>
        </header>
        <div className="tableWrap">
          <table className="table executionLimitTable" data-ui-guide="execution-limit-policies">
            <thead>
              <tr>
                <th>{translate("executionLimits.target")}</th>
                <th>{translate("executionLimits.type")}</th>
                <th>{translate("executionLimits.releaseCeiling")}</th>
                <th>{translate("executionLimits.operatorAllowance")}</th>
                <th>{translate("executionLimits.effectiveLimit")}</th>
                <th>{translate("common.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {readback.enforced.active_release.map((shape) => {
                const identity = executionLimitIdentity(shape);
                const policy = activePolicyForShape(readback, shape);
                const busy = busyKey === identity;
                const savedAllowance = policy?.operator_allowance ?? "";
                const changed = allowances[identity] !== String(savedAllowance);
                return (
                  <tr key={`${identity}:${shape.shape_fingerprint}`}>
                    <TargetCell item={shape} actionNames={actionNames} />
                    <LimitTypeCell item={shape} />
                    <LimitValueCell
                      label={translate("executionLimits.releaseCeiling")}
                      value={shape.release_ceiling}
                      item={shape}
                      emptyLabel={translate("executionLimits.noReleaseCeiling")}
                    />
                    <td data-label={translate("executionLimits.operatorAllowance")}>
                      <label className="executionLimitAllowanceField">
                        <span className="visuallyHidden">
                          {translate("executionLimits.allowanceFor", {
                            target: targetLabel(shape, actionNames),
                          })}
                        </span>
                        <input
                          type="number"
                          min={1}
                          max={2_147_483_647}
                          step={1}
                          inputMode="numeric"
                          value={allowances[identity] ?? ""}
                          placeholder={translate("executionLimits.releaseDefault")}
                          disabled={busy}
                          onChange={(event) => onAllowanceChange(shape, event.target.value)}
                        />
                        <span className="cellSub">
                          {shape.kind === "rate" && shape.window_seconds
                            ? translate("executionLimits.perWindow", {
                                seconds: shape.window_seconds,
                              })
                            : translate("executionLimits.positiveOnly")}
                        </span>
                      </label>
                    </td>
                    <LimitValueCell
                      label={translate("executionLimits.effectiveLimit")}
                      value={shape.effective_limit}
                      item={shape}
                      emptyLabel={translate("executionLimits.unlimited")}
                      emphasized
                      draining={shape.over_allowance_drain}
                    />
                    <td data-label={translate("common.actions")}>
                      <div className="executionLimitActions">
                        <button
                          className="button small"
                          type="button"
                          disabled={busy || !allowances[identity] || !changed}
                          onClick={() => onSave(shape)}
                        >
                          {busy ? translate("common.saving") : translate("common.apply")}
                        </button>
                        {policy ? (
                          <button
                            className="button small danger"
                            type="button"
                            disabled={busy}
                            onClick={() => onRemove(policy)}
                          >
                            {translate("executionLimits.useReleaseDefault")}
                          </button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <p className="fieldHint">{translate("executionLimits.minimumHint")}</p>
      </section>

      {dormantPolicies.length ? (
        <DormantPolicies
          policies={dormantPolicies}
          residuals={readback.enforced.residual_cohorts}
          actionNames={actionNames}
          busyKey={busyKey}
          onRemove={onRemove}
        />
      ) : null}

      {readback.enforced.residual_cohorts.length ? (
        <ResidualCohorts
          residuals={readback.enforced.residual_cohorts}
          activeShapes={readback.enforced.active_release}
          actionNames={actionNames}
        />
      ) : null}
    </div>
  );
}

function ExecutionLimitMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="executionLimitMetric">
      <span>{label}</span>
      <strong className="mono">{value}</strong>
    </div>
  );
}

function DormantPolicies({
  policies,
  residuals,
  actionNames,
  busyKey,
  onRemove,
}: {
  policies: ExecutionLimitPolicy[];
  residuals: ExecutionLimitResidual[];
  actionNames: Map<string, string>;
  busyKey: string;
  onRemove: (policy: ExecutionLimitPolicy) => void;
}) {
  return (
    <section className="executionLimitSection" aria-labelledby="dormantExecutionLimitsTitle">
      <header className="executionLimitSectionHeader">
        <div>
          <h3 id="dormantExecutionLimitsTitle">{translate("executionLimits.dormantTitle")}</h3>
          <p>{translate("executionLimits.dormantHint")}</p>
        </div>
        <span className="badge badge-warning">
          {translate("executionLimits.policyCount", { count: policies.length })}
        </span>
      </header>
      <div className="tableWrap">
        <table className="table executionLimitTable">
          <thead>
            <tr>
              <th>{translate("executionLimits.target")}</th>
              <th>{translate("executionLimits.type")}</th>
              <th>{translate("executionLimits.operatorAllowance")}</th>
              <th>{translate("executionLimits.previousJobs")}</th>
              <th>{translate("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {policies.map((policy) => {
              const residual = residuals.filter(
                (item) =>
                  executionLimitIdentity(item) === executionLimitIdentity(policy) &&
                  item.shape_fingerprint === policy.shape_fingerprint,
              );
              const jobs = residual.reduce((sum, item) => sum + item.queued + item.running, 0);
              const busy = busyKey === executionLimitIdentity(policy);
              return (
                <tr key={`${executionLimitIdentity(policy)}:${policy.shape_fingerprint}`}>
                  <TargetCell item={policy} actionNames={actionNames} />
                  <LimitTypeCell item={policy} />
                  <LimitValueCell
                    label={translate("executionLimits.operatorAllowance")}
                    value={policy.operator_allowance}
                    item={policy}
                    emptyLabel="—"
                  />
                  <td data-label={translate("executionLimits.previousJobs")}>
                    <strong className="mono">{jobs}</strong>
                  </td>
                  <td data-label={translate("common.actions")}>
                    <button
                      className="button small danger"
                      type="button"
                      disabled={busy}
                      onClick={() => onRemove(policy)}
                    >
                      {busy ? translate("common.saving") : translate("common.delete")}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function ResidualCohorts({
  residuals,
  activeShapes,
  actionNames,
}: {
  residuals: ExecutionLimitResidual[];
  activeShapes: EnforcedExecutionLimit[];
  actionNames: Map<string, string>;
}) {
  return (
    <section className="executionLimitSection" aria-labelledby="residualExecutionLimitsTitle">
      <header className="executionLimitSectionHeader">
        <div>
          <h3 id="residualExecutionLimitsTitle">{translate("executionLimits.residualTitle")}</h3>
          <p>{translate("executionLimits.residualHint")}</p>
        </div>
      </header>
      <div className="tableWrap">
        <table className="table executionLimitTable" data-ui-guide="execution-limit-residuals">
          <thead>
            <tr>
              <th>{translate("executionLimits.target")}</th>
              <th>{translate("executionLimits.cohort")}</th>
              <th>{translate("executionLimits.pinnedCeiling")}</th>
              <th>{translate("executionLimits.operatorAllowance")}</th>
              <th>{translate("executionLimits.effectiveLimit")}</th>
              <th>{translate("executionLimits.queuedRunning")}</th>
            </tr>
          </thead>
          <tbody>
            {residuals.map((residual) => (
              <tr
                key={`${executionLimitIdentity(residual)}:${residual.shape_fingerprint}:${residual.release_ceiling}`}
              >
                <TargetCell item={residual} actionNames={actionNames} />
                <td data-label={translate("executionLimits.cohort")}>
                  <span className="executionLimitCohort">
                    <span
                      className={`badge ${residualMatchesActiveShape(residual, activeShapes) ? "badge-good" : "badge-warning"}`}
                    >
                      {translate(
                        residualMatchesActiveShape(residual, activeShapes)
                          ? "executionLimits.cohortActive"
                          : "executionLimits.cohortPrevious",
                      )}
                    </span>
                    <code className="mono" title={residual.shape_fingerprint}>
                      {shortFingerprint(residual.shape_fingerprint)}
                    </code>
                  </span>
                </td>
                <LimitValueCell
                  label={translate("executionLimits.pinnedCeiling")}
                  value={residual.release_ceiling}
                  item={residual}
                  emptyLabel={translate("executionLimits.noReleaseCeiling")}
                />
                <LimitValueCell
                  label={translate("executionLimits.operatorAllowance")}
                  value={residual.operator_allowance}
                  item={residual}
                  emptyLabel={translate("executionLimits.releaseDefault")}
                />
                <LimitValueCell
                  label={translate("executionLimits.effectiveLimit")}
                  value={residual.effective_limit}
                  item={residual}
                  emptyLabel={translate("executionLimits.unlimited")}
                  emphasized
                  draining={residual.over_allowance_drain}
                />
                <td data-label={translate("executionLimits.queuedRunning")}>
                  <strong className="mono">
                    {residual.queued} / {residual.running}
                  </strong>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

type ExecutionLimitDisplayItem = Pick<
  EnforcedExecutionLimit,
  "scope" | "action_key" | "policy_id" | "kind" | "window_seconds"
>;

function TargetCell({
  item,
  actionNames,
}: {
  item: ExecutionLimitDisplayItem;
  actionNames: Map<string, string>;
}) {
  return (
    <td data-label={translate("executionLimits.target")}>
      <div className="executionLimitAction">
        <span className="cellTitle">{targetLabel(item, actionNames)}</span>
        <span className="cellSub mono">{item.policy_id}</span>
      </div>
    </td>
  );
}

function LimitTypeCell({ item }: { item: ExecutionLimitDisplayItem }) {
  return (
    <td data-label={translate("executionLimits.type")}>
      <span className="badge badge-neutral">{translate(`executionLimits.type.${item.kind}`)}</span>
    </td>
  );
}

function LimitValueCell({
  label,
  value,
  item,
  emptyLabel,
  emphasized,
  draining,
}: {
  label: string;
  value: number | null;
  item: Pick<ExecutionLimitDisplayItem, "kind" | "window_seconds">;
  emptyLabel: string;
  emphasized?: boolean;
  draining?: boolean;
}) {
  return (
    <td data-label={label}>
      <span className="executionLimitCapacityValue">
        <strong className={`executionLimitCapacity mono${emphasized ? " isEffective" : ""}`}>
          {value ?? emptyLabel}
        </strong>
        {value !== null && item.kind === "rate" && item.window_seconds ? (
          <span className="cellSub">
            {translate("executionLimits.perWindow", { seconds: item.window_seconds })}
          </span>
        ) : null}
        {draining ? (
          <span className="badge badge-warning">{translate("executionLimits.draining")}</span>
        ) : null}
      </span>
    </td>
  );
}

function targetLabel(item: ExecutionLimitDisplayItem, actionNames: Map<string, string>): string {
  if (item.scope === "action" && item.action_key) {
    return actionNames.get(item.action_key) || item.action_key;
  }
  return translate("executionLimits.allActions");
}

export function executionLimitIdentity(item: ExecutionLimitDisplayItem): string {
  return [item.scope, item.action_key || "", item.kind, item.policy_id].join(":");
}

export function activePolicyForShape(
  readback: ExecutionLimitPolicyReadback,
  shape: EnforcedExecutionLimit,
): ExecutionLimitPolicy | undefined {
  const identity = executionLimitIdentity(shape);
  return readback.desired.items.find(
    (policy) =>
      policy.status === "applied" &&
      executionLimitIdentity(policy) === identity &&
      policy.shape_fingerprint === shape.shape_fingerprint,
  );
}

export function residualMatchesActiveShape(
  residual: ExecutionLimitResidual,
  activeShapes: EnforcedExecutionLimit[],
): boolean {
  return activeShapes.some(
    (shape) =>
      executionLimitIdentity(shape) === executionLimitIdentity(residual) &&
      shape.shape_fingerprint === residual.shape_fingerprint &&
      shape.release_ceiling === residual.release_ceiling,
  );
}

export function effectiveExecutionLimit(
  releaseCeiling: number | null,
  operatorAllowance: number | null,
): number | null {
  if (releaseCeiling === null) return operatorAllowance;
  if (operatorAllowance === null) return releaseCeiling;
  return Math.min(releaseCeiling, operatorAllowance);
}

export function shortFingerprint(value: string): string {
  if (!value) return "—";
  const digest = value.split(":").at(-1) || value;
  return digest.length > 12 ? `${digest.slice(0, 12)}…` : digest;
}

export function concurrencyLimits(
  limits: AppDetail["app"]["execution_limits"] | ActionView["execution_limits"],
): KeyedConcurrencyLimit[] {
  return limits?.concurrency || [];
}

export function rateLimits(
  limits: AppDetail["app"]["execution_limits"] | ActionView["execution_limits"],
): KeyedRateLimit[] {
  return limits?.rate || [];
}

export function executionLimitRows(limits?: ExecutionLimits): ExecutionLimitRow[] {
  return [
    ...concurrencyLimits(limits).map(
      (limit): ExecutionLimitRow => ({ kind: "concurrency", limit }),
    ),
    ...rateLimits(limits).map((limit): ExecutionLimitRow => ({ kind: "rate", limit })),
  ];
}

export function actionExecutionLimitRows(actions: ActionView[]): ActionLimitRow[] {
  return [...actions]
    .sort((left, right) =>
      left.action_key.localeCompare(right.action_key, undefined, { numeric: true }),
    )
    .flatMap((action) => {
      const displayName = actionDisplayName(action.display_name);
      return executionLimitRows(action.execution_limits).map((row) => ({
        actionKey: action.action_key,
        actionName: displayName || action.action_key,
        ...row,
      }));
    });
}
