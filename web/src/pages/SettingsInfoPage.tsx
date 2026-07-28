import { CheckCircle2, CircleAlert, ServerCog } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { DefinitionList, ErrorNotice, Loading, Panel } from "../components/ui";
import { errorMessage, type SystemInfo } from "../lib/api";
import { useApp } from "../lib/app-context";
import { translate } from "../shared/i18n";

export function SettingsInfoPage() {
  const { api, settings } = useApp();
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const browserItems: Array<[string, ReactNode]> = [
    [translate("settingsNav.workspace"), settings.workspace || "default"],
    [translate("settings.actor"), settings.actor || translate("info.notSet")],
    [
      translate("settings.apiToken"),
      settings.token ? translate("info.configuredInBrowser") : translate("common.notConfigured"),
    ],
    [translate("info.apiBase"), `/api/w/${encodeURIComponent(settings.workspace || "default")}`],
  ];

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
      title={translate("navigation.settings")}
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
                  <span>{info.service}</span>
                </div>
              </div>
              <DefinitionList
                items={[
                  [translate("info.service"), info.service],
                  [translate("settingsNav.workspace"), info.workspace],
                  [
                    translate("info.readiness"),
                    info.ready ? translate("info.ready") : translate("info.notReady"),
                  ],
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
                  labelize(key),
                  formatSystemInfoValue(value),
                ])}
              />
            </Panel>
          </div>
        </>
      ) : null}

      <Panel
        title={translate("info.browserSettings")}
        subtitle={translate("info.browserSettingsHint")}
      >
        <DefinitionList items={browserItems} />
      </Panel>
    </Layout>
  );
}

function FlagList({ values }: { values: Record<string, boolean> }) {
  const entries = Object.entries(values).sort(([left], [right]) => left.localeCompare(right));
  return (
    <div className="settingsInfoFlags">
      {entries.map(([key, enabled]) => (
        <div className="settingsInfoFlag" key={key}>
          <span className={enabled ? "badge badge-good" : "badge badge-neutral"}>
            {enabled ? translate("common.enabled") : translate("info.notEnabled")}
          </span>
          <strong>{labelize(key)}</strong>
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

export function formatSystemInfoValue(value: unknown): string {
  if (typeof value === "boolean") {
    return value ? translate("common.enabled") : translate("info.notEnabled");
  }
  if (value === null || value === undefined || value === "") return "—";
  return String(value);
}
