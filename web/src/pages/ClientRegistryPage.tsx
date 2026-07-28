import { useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { EmptyState, ErrorNotice, Loading } from "../components/ui";
import { ClientDialog } from "../features/ClientDialog";
import type { Client } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, formatTime } from "../lib/format";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";

export function ClientRegistryPage() {
  const { api } = useApp();
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<Client | "new" | null>(null);
  const state = useAsync(() => api.clients(), [api]);

  const clients = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!state.data || !query) return state.data || [];
    return state.data.filter((client) => client.name.toLowerCase().includes(query));
  }, [search, state.data]);

  function finishChange() {
    setEditing(null);
    state.reload();
  }

  return (
    <Layout
      title={translate("navigation.clientRegistry")}
      subtitle={translate("clients.subtitle")}
      actions={
        <>
          <input
            className="searchInput"
            placeholder={translate("clients.filter")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            aria-label={translate("clients.filter")}
          />
          <button className="button" type="button" onClick={() => state.reload()}>
            {translate("common.refresh")}
          </button>
          <button className="button primary" type="button" onClick={() => setEditing("new")}>
            {translate("clients.register")}
          </button>
        </>
      }
    >
      <div className="inlineNotice">{translate("clients.tokenNotice")}</div>
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading /> : null}
      {state.data ? (
        clients.length === 0 ? (
          <EmptyState
            title={search ? translate("clients.noMatches") : translate("clients.empty")}
          />
        ) : (
          <div className="tableWrap">
            <table className="table" id="clientList">
              <thead>
                <tr>
                  <th>{translate("common.name")}</th>
                  <th>{translate("settings.apiToken")}</th>
                  <th>{translate("common.updated")}</th>
                  <th>{translate("common.updatedBy")}</th>
                  <th aria-label={translate("common.rowActions")} />
                </tr>
              </thead>
              <tbody>
                {clients.map((client) => (
                  <tr key={client.id}>
                    <td>
                      <Link className="cellTitle" to={`/clients/${client.id}`}>
                        {client.name}
                      </Link>
                    </td>
                    <td>
                      {client.has_token
                        ? translate("workspace.status.active")
                        : translate("clients.notIssued")}
                    </td>
                    <td title={formatTime(client.updated_at)}>
                      <span className="cellTitle">{formatRelative(client.updated_at)}</span>
                      <span className="cellSub">{formatTime(client.updated_at)}</span>
                    </td>
                    <td>{client.updated_by}</td>
                    <td className="rowActions">
                      <button
                        className="button small"
                        type="button"
                        onClick={() => setEditing(client)}
                      >
                        {translate("common.edit")}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      ) : null}

      {editing ? (
        <ClientDialog
          client={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
          onSaved={finishChange}
          onDeleted={finishChange}
        />
      ) : null}
    </Layout>
  );
}
