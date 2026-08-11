import { Panel } from "../components/ui";
import { actionDisplayName } from "../lib/action-label";
import type { ActionView, AppDetail, KeyedConcurrencyLimit } from "../lib/api";
import { translate } from "../shared/i18n";

type ActionLimitRow = {
  actionKey: string;
  actionName: string;
  limit: KeyedConcurrencyLimit;
};

export function ExecutionLimitsPanel({ detail }: { detail: AppDetail }) {
  const appLimits = concurrencyLimits(detail.app.execution_limits);
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
                  <th>{translate("executionLimits.policy")}</th>
                  <th>{translate("executionLimits.capacity")}</th>
                  <th>{translate("executionLimits.keyInput")}</th>
                </tr>
              </thead>
              <tbody>
                {actionRows.map((row) => (
                  <tr key={`${row.actionKey}:${row.limit.id}`}>
                    <td data-label={translate("executionLimits.action")}>
                      <div className="executionLimitAction">
                        <span className="cellTitle">{row.actionName}</span>
                        <span className="cellSub mono">{row.actionKey}</span>
                      </div>
                    </td>
                    <LimitCells limit={row.limit} />
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
  limits: KeyedConcurrencyLimit[];
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
                <th>{translate("executionLimits.policy")}</th>
                <th>{translate("executionLimits.capacity")}</th>
                <th>{translate("executionLimits.keyInput")}</th>
              </tr>
            </thead>
            <tbody>
              {limits.map((limit) => (
                <tr key={limit.id}>
                  <LimitCells limit={limit} />
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

function LimitCells({ limit }: { limit: KeyedConcurrencyLimit }) {
  return (
    <>
      <td data-label={translate("executionLimits.policy")}>
        <code className="mono">{limit.id}</code>
      </td>
      <td data-label={translate("executionLimits.capacity")}>
        <span className="executionLimitCapacityValue">
          <strong className="executionLimitCapacity mono">{limit.max_concurrent}</strong>
          <span className="cellSub">{translate("executionLimits.concurrentRuns")}</span>
        </span>
      </td>
      <td data-label={translate("executionLimits.keyInput")}>
        <div className="executionLimitPointers">
          {limit.input_pointers.map((pointer) => (
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

export function actionExecutionLimitRows(actions: ActionView[]): ActionLimitRow[] {
  return [...actions]
    .sort((left, right) =>
      left.action_key.localeCompare(right.action_key, undefined, { numeric: true }),
    )
    .flatMap((action) => {
      const displayName = actionDisplayName(action.display_name);
      return concurrencyLimits(action.execution_limits).map((limit) => ({
        actionKey: action.action_key,
        actionName: displayName || action.action_key,
        limit,
      }));
    });
}
