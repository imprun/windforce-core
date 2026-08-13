import { useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { DefinitionList, EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import { AuditEventTable } from "../features/AuditEventTable";
import { ClientDialog } from "../features/ClientDialog";
import { ClientInputSettings } from "../features/ClientInputSettings";
import { ClientInvocationPolicy } from "../features/ClientInvocationPolicy";
import type { Client, InputConfig } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { groupInputSettings } from "../lib/input-setting-groups";
import { Link, useRouter } from "../lib/router";
import { translate } from "../shared/i18n";

const tabs = [
  { key: "overview", label: "clients.tab.overview" },
  { key: "input-settings", label: "clients.tab.inputSettings" },
  { key: "audit", label: "clients.tab.audit" },
] as const;

type TabKey = (typeof tabs)[number]["key"];

export function ClientDetailPage({
  clientID,
  tab,
  appKey,
}: {
  clientID: string;
  tab: string;
  appKey?: string;
}) {
  const { api } = useApp();
  const { navigate } = useRouter();
  const [editingClient, setEditingClient] = useState(false);
  const activeTab = (tabs.find((item) => item.key === tab)?.key || "overview") as TabKey;
  const state = useAsync(async () => {
    const [client, configs, apps] = await Promise.all([
      api.client(clientID),
      api.clientInputConfigs(clientID),
      api.apps(),
    ]);
    return { client, configs, apps: apps.apps || [] };
  }, [api, clientID]);

  if (state.loading && !state.data)
    return (
      <Layout title={translate("navigation.clientRegistry")}>
        <Loading />
      </Layout>
    );
  if (state.error || !state.data) {
    return (
      <Layout title={translate("clients.notFound")}>
        <ErrorNotice
          message={state.error || translate("clients.notFoundDetail")}
          onRetry={state.reload}
        />
      </Layout>
    );
  }

  const { client, configs, apps } = state.data;
  return (
    <Layout
      title={client.name}
      subtitle={translate("clients.detailSubtitle")}
      actions={
        <>
          <Link className="button" to="/clients">
            {translate("clients.backToRegistry")}
          </Link>
          <button className="button" type="button" onClick={() => setEditingClient(true)}>
            {translate("clients.editClient")}
          </button>
        </>
      }
    >
      <nav className="tabBar" aria-label={translate("clients.detailTabs")}>
        {tabs.map((item) => (
          <Link
            key={item.key}
            className={item.key === activeTab ? "tab active" : "tab"}
            to={
              item.key === "overview"
                ? `/clients/${client.id}`
                : `/clients/${client.id}/${item.key}`
            }
          >
            {translate(item.label)}
          </Link>
        ))}
      </nav>

      {activeTab === "overview" ? (
        <ClientOverview client={client} configs={configs} onUpdated={state.reload} />
      ) : null}
      {activeTab === "input-settings" ? (
        <ClientInputSettings
          client={client}
          configs={configs}
          apps={apps}
          selectedAppKey={appKey}
          onChanged={state.reload}
        />
      ) : null}
      {activeTab === "audit" ? <ClientAudit clientID={client.id} /> : null}

      {editingClient ? (
        <ClientDialog
          client={client}
          onClose={() => setEditingClient(false)}
          onSaved={() => {
            setEditingClient(false);
            state.reload();
          }}
          onDeleted={() => navigate("/clients")}
        />
      ) : null}
    </Layout>
  );
}

function ClientOverview({
  client,
  configs,
  onUpdated,
}: {
  client: Client;
  configs: InputConfig[];
  onUpdated: () => void;
}) {
  const groups = useMemo(() => groupInputSettings(configs, (config) => config.app_key), [configs]);
  const latest = useMemo(
    () =>
      configs.reduce(
        (current, config) =>
          !current || Date.parse(config.updated_at) > Date.parse(current.updated_at)
            ? config
            : current,
        undefined as (typeof configs)[number] | undefined,
      ),
    [configs],
  );
  return (
    <>
      <Panel title={translate("clients.identity")} subtitle={translate("clients.identityHint")}>
        <DefinitionList
          items={[
            [translate("common.name"), client.name],
            [
              translate("clients.apiToken"),
              client.has_token
                ? translate("workspace.status.active")
                : translate("clients.notIssued"),
            ],
            [translate("common.updated"), formatTime(client.updated_at)],
            [translate("common.updatedBy"), client.updated_by],
          ]}
        />
      </Panel>
      <ClientInvocationPolicy client={client} onUpdated={onUpdated} />
      <Panel
        title={translate("clients.configurationSummary")}
        subtitle={translate("clients.configurationSummaryHint")}
      >
        {configs.length ? (
          <DefinitionList
            items={[
              [translate("clients.configuredApps"), groups.length],
              [translate("clients.actionScopes"), configs.length],
              [
                translate("clients.configuredValues"),
                groups.reduce((total, group) => total + group.valueCount, 0),
              ],
              [
                translate("clients.lockedValues"),
                groups.reduce((total, group) => total + group.lockedCount, 0),
              ],
              [
                translate("clients.lastSettingsChange"),
                latest ? `${formatRelative(latest.updated_at)} · ${latest.updated_by}` : "—",
              ],
            ]}
          />
        ) : (
          <EmptyState title={translate("clients.noInputSettings")} />
        )}
      </Panel>
    </>
  );
}

function ClientAudit({ clientID }: { clientID: string }) {
  const { api } = useApp();
  const state = useAsync(() => api.auditEvents({ clientID, limit: 250 }), [api, clientID]);
  return (
    <Panel title={translate("clients.auditTrail")} subtitle={translate("clients.auditTrailHint")}>
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading /> : null}
      {state.data ? (
        <AuditEventTable events={state.data} emptyTitle={translate("clients.noChanges")} />
      ) : null}
    </Panel>
  );
}
