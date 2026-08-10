import { CheckCircle2, CircleAlert, ServerCog } from "lucide-react";
import { useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { DefinitionList, ErrorNotice, Loading, Panel } from "../components/ui";
import { errorMessage, type SystemInfo } from "../lib/api";
import { useApp } from "../lib/app-context";
import { type TranslationKey, translate } from "../shared/i18n";

const SYSTEM_INFO_LABELS: Record<string, TranslationKey> = {
  admin_token_configured: "info.label.adminToken",
  artifact_store: "info.label.artifactStore",
  catalog: "info.label.catalog",
  control_api: "info.label.controlAPI",
  execution_bundles: "info.label.executionBundles",
  git_sources: "info.label.gitSources",
  http_route_provider: "info.label.httpRouteProvider",
  http_routes: "info.label.httpRoutes",
  http_routes_count: "info.label.httpRoutesCount",
  http_routes_error: "info.label.httpRoutesError",
  http_routes_ready: "info.label.httpRoutesReady",
  invocation_api: "info.label.invocationAPI",
  job_token_configured: "info.label.jobToken",
  managed_workspaces: "info.label.managedWorkspaces",
  metrics: "info.label.metrics",
  previous_secret_key: "info.label.previousSecretKey",
  sample_root: "info.label.sampleRoot",
  schedules_count: "info.label.schedulesCount",
  secret_key_configured: "info.label.secretKey",
  state_store: "info.label.stateStore",
  syncer: "info.label.syncer",
  trigger_api: "info.label.triggerAPI",
  triggers_count: "info.label.triggersCount",
  wait_ms: "info.label.waitMs",
  web_ui: "info.label.webUI",
  ui_mode: "info.label.uiMode",
  worker_group_operator: "info.label.workerGroupOperator",
  worker_api: "info.label.workerAPI",
  worker_token_configured: "info.label.workerToken",
};

export function SettingsInfoPage() {
  const { api, runtimeConfig } = useApp();
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  async function loadInfo() {
    setLoading(true);
    setError("");
    try {
      setInfo(await api.systemInfo());
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    api
      .systemInfo()
      .then((data) => {
        if (active) setInfo(data);
      })
      .catch((cause) => {
        if (active) setError(errorMessage(cause));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api]);

  return (
    <Layout
      title={translate("info.pageTitle")}
      subtitle={translate("info.subtitle")}
      actions={
        <button
          className="button"
          type="button"
          onClick={() => void loadInfo()}
          title={translate("info.refresh")}
        >
          <ServerCog aria-hidden="true" />
          {translate("common.refresh")}
        </button>
      }
    >
      <SettingsNav />
      <Panel
        title={translate("info.workerGroupOperations")}
        subtitle={translate("info.workerGroupOperationsHint")}
      >
        <div className="flex flex-col gap-4">
          <DefinitionList
            items={[
              [
                translate("info.workerGroupOperator"),
                runtimeConfig?.workerGroupOperator === "external"
                  ? translate("info.workerGroupOperator.external")
                  : translate("info.workerGroupOperator.selfManaged"),
              ],
              [translate("info.uiMode"), translate("info.uiMode.embedded")],
            ]}
          />
          <p className="text-sm leading-6 text-muted-foreground">
            {runtimeConfig?.workerGroupOperator === "external"
              ? translate("info.workerGroupOperator.externalBody")
              : translate("info.workerGroupOperator.selfManagedBody")}
          </p>
          {runtimeConfig?.workerGroupOperator === "external" ? (
            runtimeConfig.hostConsole ? (
              <div>
                <a className="button primary no-underline" href={runtimeConfig.hostConsole.url}>
                  {runtimeConfig.hostConsole.label}
                </a>
              </div>
            ) : (
              <span className="inlineNotice">
                {translate("info.workerGroupOperator.hostUnavailable")}
              </span>
            )
          ) : null}
          <p className="text-xs leading-5 text-muted-foreground">
            {translate("info.workerGroupOperator.metadataOnly")}
          </p>
        </div>
      </Panel>
      {error ? <ErrorNotice message={error} onRetry={() => void loadInfo()} /> : null}
      {loading && !info ? <Loading label={translate("info.loading")} /> : null}
      {info ? (
        <>
          <Panel title={translate("info.service")} subtitle={translate("info.serviceHint")}>
            <div className="settingsInfoHero">
              <div
                className={info.ready ? "settingsInfoStatus good" : "settingsInfoStatus warning"}
              >
                {info.ready ? (
                  <CheckCircle2 aria-hidden="true" />
                ) : (
                  <CircleAlert aria-hidden="true" />
                )}
                <div>
                  <strong>
                    {info.ready ? translate("info.ready") : translate("info.notReady")}
                  </strong>
                  <span>{translate("info.controlPlaneStatus")}</span>
                </div>
              </div>
              <DefinitionList
                items={[
                  [translate("info.serviceID"), info.service],
                  [translate("settingsNav.workspace"), info.workspace],
                ]}
              />
            </div>
          </Panel>

          <div className="settingsInfoGrid">
            <Panel
              title={translate("info.enabledSurfaces")}
              subtitle={translate("info.enabledSurfacesHint")}
            >
              <FlagList values={info.planes} />
            </Panel>
            <Panel
              title={translate("info.backendAvailability")}
              subtitle={translate("info.backendAvailabilityHint")}
            >
              <FlagList values={info.backends} />
            </Panel>
          </div>

          <div className="settingsInfoGrid">
            <Panel
              title={translate("info.authSecrets")}
              subtitle={translate("info.authSecretsHint")}
            >
              <FlagList values={info.auth} />
            </Panel>
            <Panel
              title={translate("info.runtimeConfig")}
              subtitle={translate("info.runtimeConfigHint")}
            >
              <DefinitionList
                items={Object.entries(info.runtime_config).map(([key, value]) => [
                  systemInfoLabel(key),
                  formatSystemInfoValue(value),
                ])}
              />
            </Panel>
          </div>
        </>
      ) : null}
    </Layout>
  );
}

function FlagList({ values }: { values: Record<string, boolean> }) {
  const entries = Object.entries(values).sort(([left], [right]) => left.localeCompare(right));
  return (
    <div className="settingsInfoFlags">
      {entries.map(([key, enabled]) => (
        <div className="settingsInfoFlag" key={key}>
          <strong>{systemInfoLabel(key)}</strong>
          <span className={enabled ? "badge badge-good" : "badge badge-neutral"}>
            {enabled ? translate("common.enabled") : translate("info.notEnabled")}
          </span>
        </div>
      ))}
    </div>
  );
}

function labelize(key: string): string {
  return key
    .split("_")
    .filter(Boolean)
    .map((part) => part[0]?.toUpperCase() + part.slice(1))
    .join(" ");
}

export function systemInfoLabel(key: string): string {
  const translationKey = SYSTEM_INFO_LABELS[key];
  return translationKey ? translate(translationKey) : labelize(key);
}

export function formatSystemInfoValue(value: unknown): string {
  if (typeof value === "boolean") {
    return value ? translate("common.enabled") : translate("info.notEnabled");
  }
  if (value === null || value === undefined || value === "") return "—";
  return String(value);
}
