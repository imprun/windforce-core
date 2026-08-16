import { Panel } from "../components/ui";
import { actionDisplayName } from "../lib/action-label";
import type {
  ActionView,
  AppDetail,
  ExecutionLimits,
  KeyedConcurrencyLimit,
  KeyedRateLimit,
} from "../lib/api";
import { translate } from "../shared/i18n";

type ActionLimitRow = {
  actionKey: string;
  actionName: string;
} & ExecutionLimitRow;

export type ExecutionLimitRow =
  | { kind: "concurrency"; limit: KeyedConcurrencyLimit }
  | { kind: "rate"; limit: KeyedRateLimit };

export function ExecutionLimitsPanel({ detail }: { detail: AppDetail }) {
  const appLimits = executionLimitRows(detail.app.execution_limits);
  const actionRows = actionExecutionLimitRows(detail.actions);
  const total = appLimits.length + actionRows.length;

  return (
    <Panel
      title={translate("executionLimits.title")}
      subtitle={translate("executionLimits.subtitle")}
    >
      <div className="executionLimitsIntro">
        <div>
          <span className="badge badge-neutral">{translate("executionLimits.activeRelease")}</span>
          <strong>
            {translate("executionLimits.policyCount", {
              count: total,
            })}
          </strong>
        </div>
        <p>{translate("executionLimits.changeHint")}</p>
        <p className="fieldHint">{translate("executionLimits.cumulativeHint")}</p>
      </div>

      <ExecutionLimitSection
        title={translate("executionLimits.appScope")}
        description={translate("executionLimits.appScopeHint")}
        empty={translate("executionLimits.noAppPolicies")}
        limits={appLimits}
      />

      <section className="executionLimitSection" aria-labelledby="actionExecutionLimitsTitle">
        <header className="executionLimitSectionHeader">
          <div>
            <h3 id="actionExecutionLimitsTitle">{translate("executionLimits.actionScope")}</h3>
            <p>{translate("executionLimits.actionScopeHint")}</p>
          </div>
          <span className="badge badge-neutral">
            {translate("executionLimits.policyCount", { count: actionRows.length })}
          </span>
        </header>
        {actionRows.length ? (
          <div className="tableWrap">
            <table className="table executionLimitTable" data-ui-guide="action-execution-limits">
              <thead>
                <tr>
                  <th>{translate("executionLimits.action")}</th>
                  <th>{translate("executionLimits.type")}</th>
                  <th>{translate("executionLimits.policy")}</th>
                  <th>{translate("executionLimits.capacity")}</th>
                  <th>{translate("executionLimits.keyInput")}</th>
                </tr>
              </thead>
              <tbody>
                {actionRows.map((row) => (
                  <tr key={`${row.actionKey}:${row.kind}:${row.limit.id}`}>
                    <td data-label={translate("executionLimits.action")}>
                      <div className="executionLimitAction">
                        <span className="cellTitle">{row.actionName}</span>
                        <span className="cellSub mono">{row.actionKey}</span>
                      </div>
                    </td>
                    <LimitCells row={row} />
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="executionLimitsEmpty">{translate("executionLimits.noActionPolicies")}</p>
        )}
      </section>
    </Panel>
  );
}

function ExecutionLimitSection({
  title,
  description,
  empty,
  limits,
}: {
  title: string;
  description: string;
  empty: string;
  limits: ExecutionLimitRow[];
}) {
  return (
    <section className="executionLimitSection" aria-labelledby="appExecutionLimitsTitle">
      <header className="executionLimitSectionHeader">
        <div>
          <h3 id="appExecutionLimitsTitle">{title}</h3>
          <p>{description}</p>
        </div>
        <span className="badge badge-neutral">
          {translate("executionLimits.policyCount", { count: limits.length })}
        </span>
      </header>
      {limits.length ? (
        <div className="tableWrap">
          <table className="table executionLimitTable" data-ui-guide="app-execution-limits">
            <thead>
              <tr>
                <th>{translate("executionLimits.type")}</th>
                <th>{translate("executionLimits.policy")}</th>
                <th>{translate("executionLimits.capacity")}</th>
                <th>{translate("executionLimits.keyInput")}</th>
              </tr>
            </thead>
            <tbody>
              {limits.map((row) => (
                <tr key={`${row.kind}:${row.limit.id}`}>
                  <LimitCells row={row} />
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="executionLimitsEmpty">{empty}</p>
      )}
    </section>
  );
}

function LimitCells({ row }: { row: ExecutionLimitRow }) {
  return (
    <>
      <td data-label={translate("executionLimits.type")}>
        <span className="badge badge-neutral">{translate(`executionLimits.type.${row.kind}`)}</span>
      </td>
      <td data-label={translate("executionLimits.policy")}>
        <code className="mono">{row.limit.id}</code>
      </td>
      <td data-label={translate("executionLimits.capacity")}>
        <span className="executionLimitCapacityValue">
          <strong className="executionLimitCapacity mono">
            {row.kind === "concurrency" ? row.limit.max_concurrent : row.limit.max_attempts}
          </strong>
          <span className="cellSub">
            {row.kind === "concurrency"
              ? translate("executionLimits.concurrentRuns")
              : translate("executionLimits.attemptsPerWindow", {
                  seconds: row.limit.window_seconds,
                })}
          </span>
        </span>
      </td>
      <td data-label={translate("executionLimits.keyInput")}>
        <div className="executionLimitPointers">
          {row.limit.input_pointers.map((pointer) => (
            <code className="badge badge-neutral mono" key={pointer}>
              {pointer}
            </code>
          ))}
        </div>
      </td>
    </>
  );
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
