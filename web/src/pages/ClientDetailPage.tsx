import { useState } from "react";
import { Layout } from "../components/Layout";
import { DefinitionList, ErrorNotice, Loading, Panel } from "../components/ui";
import { AuditEventTable } from "../features/AuditEventTable";
import { ClientDialog } from "../features/ClientDialog";
import { ClientInvocationPolicy } from "../features/ClientInvocationPolicy";
import type { Client } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatTime } from "../lib/format";
import { Link, useRouter } from "../lib/router";
import { translate } from "../shared/i18n";

const tabs = [
  { key: "overview", label: "clients.tab.overview" },
  { key: "audit", label: "clients.tab.audit" },
] as const;

type TabKey = (typeof tabs)[number]["key"];

export function ClientDetailPage({ clientID, tab }: { clientID: string; tab: string }) {
  const { api } = useApp();
  const { navigate } = useRouter();
  const [editingClient, setEditingClient] = useState(false);
  const activeTab = (tabs.find((item) => item.key === tab)?.key || "overview") as TabKey;
  const state = useAsync(() => api.client(clientID), [api, clientID]);

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

  const client = state.data;
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
        <ClientOverview client={client} onUpdated={state.reload} />
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

function ClientOverview({ client, onUpdated }: { client: Client; onUpdated: () => void }) {
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
