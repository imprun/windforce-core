import { type FormEvent, useState } from "react";
import { Layout } from "../components/Layout";
import { ErrorNotice, Loading, Panel, SelectControl } from "../components/ui";
import { AuditEventTable } from "../features/AuditEventTable";
import { useApp, useAsync } from "../lib/app-context";
import { translate } from "../shared/i18n";

export function AuditPage() {
  const { api } = useApp();
  const [category, setCategory] = useState("");
  const [appKey, setAppKey] = useState("");
  const [clientID, setClientID] = useState("");
  const [actorDraft, setActorDraft] = useState("");
  const [actor, setActor] = useState("");

  const options = useAsync(async () => {
    const [apps, clients] = await Promise.all([api.apps(), api.clients()]);
    return { apps: apps.apps || [], clients };
  }, [api]);
  const selectedApp = options.data?.apps.find((app) => app.app_key === appKey);
  const events = useAsync(
    () =>
      api.auditEvents({
        category,
        appKey,
        clientID,
        actor,
        gitSourceID: selectedApp?.git_source_id,
        limit: 250,
      }),
    [api, category, appKey, clientID, actor, selectedApp?.git_source_id],
  );

  function applyActor(event: FormEvent) {
    event.preventDefault();
    setActor(actorDraft.trim());
  }

  function resetFilters() {
    setCategory("");
    setAppKey("");
    setClientID("");
    setActorDraft("");
    setActor("");
  }

  const filtered = Boolean(category || appKey || clientID || actor);

  return (
    <Layout
      title={translate("navigation.audit")}
      subtitle={translate("audit.subtitle")}
      actions={
        <button className="button" type="button" onClick={() => events.reload()}>
          {translate("common.refresh")}
        </button>
      }
    >
      <Panel
        title={translate("audit.workspaceActivity")}
        subtitle={
          events.data
            ? translate("audit.recentEvents", { count: events.data.length })
            : translate("audit.loadingEvents")
        }
        actions={
          filtered ? (
            <button className="button small" type="button" onClick={resetFilters}>
              {translate("audit.resetFilters")}
            </button>
          ) : null
        }
      >
        <form className="auditFilters" onSubmit={applyActor}>
          <label className="filterField">
            <span>{translate("audit.category")}</span>
            <SelectControl
              value={category}
              onChange={setCategory}
              ariaLabel={translate("audit.category")}
              options={[
                { value: "", label: translate("audit.allCategories") },
                { value: "workspace", label: translate("settingsNav.workspace") },
                { value: "repository", label: translate("audit.repository") },
                { value: "release", label: translate("audit.release") },
                { value: "client", label: translate("navigation.clientRegistry") },
                { value: "input_settings", label: translate("audit.inputSettings") },
                {
                  value: "runtime_configuration",
                  label: translate("audit.runtimeConfiguration"),
                },
                { value: "webhook", label: translate("settingsNav.webhooks") },
              ]}
            />
          </label>
          <label className="filterField">
            <span>{translate("apps.column.app")}</span>
            <SelectControl
              value={appKey}
              onChange={setAppKey}
              ariaLabel={translate("audit.app")}
              options={[
                { value: "", label: translate("audit.allApps") },
                ...(options.data?.apps || []).map((app) => ({
                  value: app.app_key,
                  label: app.app_key,
                })),
              ]}
            />
          </label>
          <label className="filterField">
            <span>{translate("audit.client")}</span>
            <SelectControl
              value={clientID}
              onChange={setClientID}
              ariaLabel={translate("audit.client")}
              options={[
                { value: "", label: translate("audit.allClients") },
                ...(options.data?.clients || []).map((client) => ({
                  value: client.id,
                  label: client.name,
                })),
              ]}
            />
          </label>
          <label className="filterField auditActorFilter">
            <span>{translate("settings.actor")}</span>
            <span className="filterInputAction">
              <input
                value={actorDraft}
                placeholder={translate("audit.actorPlaceholder")}
                onChange={(event) => setActorDraft(event.target.value)}
              />
              <button className="button" type="submit">
                {translate("common.apply")}
              </button>
            </span>
          </label>
        </form>

        {options.error ? <ErrorNotice message={options.error} onRetry={options.reload} /> : null}
        {events.error ? <ErrorNotice message={events.error} onRetry={events.reload} /> : null}
        {events.loading && !events.data ? <Loading /> : null}
        {events.data ? <AuditEventTable events={events.data} /> : null}
      </Panel>
    </Layout>
  );
}
