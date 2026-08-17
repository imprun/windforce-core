import { Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { EmptyState, ErrorNotice, Loading, Panel } from "../components/ui";
import { actionDisplayName } from "../lib/action-label";
import type { AppDetail, InputConfig } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { appSettingsPath } from "../lib/app-settings-navigation";
import {
  groupInputSettings,
  inputSettingGroupMatches,
  paginate,
} from "../lib/input-setting-groups";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";
import { InputConfigDialog } from "./InputConfigDialog";
import { InputSettingScopeList } from "./InputSettingScopeList";
import {
  countLabel,
  InputSettingSummaryTable,
  SummaryPagination,
} from "./InputSettingSummaryTable";

const PAGE_SIZE = 25;

export function AppInputSettings({
  detail,
  sourceID,
  selectedClientID,
}: {
  detail: AppDetail;
  sourceID: number;
  selectedClientID?: string;
}) {
  const { api } = useApp();
  const appDefaultLabel = translate("inputSettings.appDefault");
  const [editing, setEditing] = useState<InputConfig | "new" | null>(null);
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const state = useAsync(async () => {
    const [configs, clients] = await Promise.all([
      api.appInputConfigs(detail.app.app_key),
      api.clients(),
    ]);
    return { configs, clients };
  }, [api, detail.app.app_key]);
  const clientsByID = useMemo(
    () => new Map((state.data?.clients || []).map((client) => [client.id, client])),
    [state.data?.clients],
  );
  const actionsByKey = useMemo(
    () => new Map(detail.actions.map((action) => [action.action_key, action])),
    [detail.actions],
  );
  const groups = useMemo(() => {
    const grouped = groupInputSettings(
      state.data?.configs || [],
      (config) => config.client_id || "",
    );
    return grouped.sort((left, right) => {
      if (!left.key) return -1;
      if (!right.key) return 1;
      const leftName = clientsByID.get(left.key)?.name || left.key;
      const rightName = clientsByID.get(right.key)?.name || right.key;
      return leftName.localeCompare(rightName);
    });
  }, [clientsByID, state.data?.configs]);
  const filteredGroups = useMemo(
    () =>
      groups.filter((group) => {
        const actionNames = group.actionKeys.flatMap((actionKey) => {
          const action = actionsByKey.get(actionKey);
          return action ? [actionDisplayName(action.display_name) || action.action_key] : [];
        });
        return inputSettingGroupMatches(group, search, [
          clientsByID.get(group.key)?.name || appDefaultLabel,
          ...actionNames,
        ]);
      }),
    [actionsByKey, appDefaultLabel, clientsByID, groups, search],
  );
  const pagedGroups = useMemo(
    () => paginate(filteredGroups, page, PAGE_SIZE),
    [filteredGroups, page],
  );

  useEffect(() => setPage(1), []);
  useEffect(() => {
    if (page !== pagedGroups.page) setPage(pagedGroups.page);
  }, [page, pagedGroups.page]);

  function finish() {
    setEditing(null);
    state.reload();
  }

  const selectedScopeKey = selectedClientID === "default" ? "" : selectedClientID;
  const selectedGroup =
    selectedClientID === undefined
      ? undefined
      : groups.find((group) => group.key === selectedScopeKey);
  const selectedClient = selectedScopeKey ? clientsByID.get(selectedScopeKey) : undefined;
  const selectedLabel = selectedScopeKey
    ? selectedClient?.name || translate("inputSettings.removedClient")
    : translate("inputSettings.allClientsDefault");
  const fixedClientID = selectedClientID === undefined ? undefined : selectedScopeKey;

  return (
    <>
      {selectedClientID === undefined ? (
        <Panel
          title={translate("audit.inputSettings")}
          subtitle={translate("inputSettings.appSubtitle")}
          actions={
            <>
              <Link className="button" to={`/apps/${sourceID}/audit`}>
                {translate("common.viewAudit")}
              </Link>
              <button className="button primary" type="button" onClick={() => setEditing("new")}>
                <Plus size={16} aria-hidden="true" />
                {translate("inputSettings.add")}
              </button>
            </>
          }
        >
          {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
          {state.loading && !state.data ? <Loading /> : null}
          {state.data ? (
            state.data.configs.length === 0 ? (
              <EmptyState title={translate("inputSettings.appEmpty")} />
            ) : (
              <>
                <div className="settingsSummaryToolbar">
                  <input
                    className="searchInput"
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={translate("inputSettings.searchApp")}
                    aria-label={translate("inputSettings.searchApp")}
                  />
                  <span>
                    {countLabel(
                      filteredGroups.length,
                      translate("inputSettings.clientScope"),
                      translate("inputSettings.clientScopes"),
                    )}
                  </span>
                </div>
                {filteredGroups.length ? (
                  <InputSettingSummaryTable
                    id="appInputSettingsSummary"
                    scopeHeading={translate("inputSettings.clientScope")}
                    rows={pagedGroups.items.map((group) => {
                      const client = group.key ? clientsByID.get(group.key) : undefined;
                      const label = group.key
                        ? client?.name || translate("inputSettings.removedClient")
                        : translate("inputSettings.allClients");
                      const actionNames = group.actionKeys.map((actionKey) => {
                        if (!actionKey) return translate("appDetail.allActions");
                        const action = actionsByKey.get(actionKey);
                        return action
                          ? actionDisplayName(action.display_name) || actionKey
                          : actionKey;
                      });
                      return {
                        group,
                        label,
                        subtitle: group.key
                          ? translate("inputSettings.clientOverride")
                          : translate("inputSettings.appDefault"),
                        href: appSettingsPath(
                          sourceID,
                          "input-settings",
                          "client",
                          group.key || "default",
                        ),
                        coverage: countLabel(
                          group.configs.length,
                          translate("inputSettings.actionScopeLower"),
                          translate("inputSettings.actionScopes"),
                        ),
                        coverageDetail: actionNames.join(", "),
                      };
                    })}
                  />
                ) : (
                  <EmptyState title={translate("inputSettings.noClientMatches")} />
                )}
                <SummaryPagination
                  page={pagedGroups.page}
                  totalPages={pagedGroups.totalPages}
                  totalItems={filteredGroups.length}
                  pageSize={PAGE_SIZE}
                  onChange={setPage}
                />
              </>
            )
          ) : null}
        </Panel>
      ) : (
        <Panel
          title={translate("inputSettings.namedSettings", { name: selectedLabel })}
          subtitle={translate("inputSettings.scopeSubtitle")}
          actions={
            <>
              <Link className="button" to={appSettingsPath(sourceID, "input-settings")}>
                {translate("inputSettings.backToClientScopes")}
              </Link>
              <button className="button primary" type="button" onClick={() => setEditing("new")}>
                <Plus size={16} aria-hidden="true" />
                {translate("inputSettings.add")}
              </button>
            </>
          }
        >
          {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
          {state.loading && !state.data ? <Loading /> : null}
          {state.data && !selectedGroup && selectedScopeKey && !selectedClient ? (
            <EmptyState title={translate("inputSettings.scopeUnavailable")} />
          ) : null}
          {state.data && !selectedGroup && (!selectedScopeKey || selectedClient) ? (
            <EmptyState title={translate("inputSettings.scopeEmpty")} />
          ) : null}
          {selectedGroup ? (
            <InputSettingScopeList
              id="appInputSettings"
              items={selectedGroup.configs.map((config) => {
                const action = config.action_key ? actionsByKey.get(config.action_key) : undefined;
                const actionName = action
                  ? actionDisplayName(action.display_name) || action.action_key
                  : translate("appDetail.allActions");
                return {
                  key: `${config.client_id || "default"}-${config.action_key || "all"}`,
                  config,
                  primaryLabel: translate("inputSettings.clientScope"),
                  primaryValue: selectedClient ? (
                    <Link to={`/clients/${selectedClient.id}`}>{selectedClient.name}</Link>
                  ) : (
                    translate("inputSettings.allClients")
                  ),
                  primaryMeta: selectedClient
                    ? translate("inputSettings.clientOverride")
                    : translate("inputSettings.appDefault"),
                  actionName,
                  actionMeta: config.action_key || translate("inputSettings.appDefault"),
                  editLabel: translate("inputSettings.editNamedAction", {
                    name: selectedLabel,
                    action: actionName,
                  }),
                  onEdit: () => setEditing(config),
                };
              })}
            />
          ) : null}
        </Panel>
      )}

      {editing && state.data ? (
        <InputConfigDialog
          appKey={detail.app.app_key}
          actions={detail.actions}
          clients={state.data.clients}
          existing={editing === "new" ? undefined : editing}
          fixedClientID={fixedClientID}
          onClose={() => setEditing(null)}
          onSaved={finish}
        />
      ) : null}
    </>
  );
}
