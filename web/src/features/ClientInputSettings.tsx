import { Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { EmptyState, ErrorNotice, Loading, Modal, Panel, SelectControl } from "../components/ui";
import { actionDisplayName } from "../lib/action-label";
import type { AppSummary, Client, InputConfig } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { groupInputSettings, inputSettingGroupMatches } from "../lib/input-setting-groups";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";
import { InputConfigDialog } from "./InputConfigDialog";
import { InputSettingScopeList } from "./InputSettingScopeList";
import { countLabel, InputSettingSummaryTable } from "./InputSettingSummaryTable";

type EditingConfig = { appKey: string; config?: InputConfig };

export function ClientInputSettings({
  client,
  configs,
  apps,
  selectedAppKey,
  onChanged,
}: {
  client: Client;
  configs: InputConfig[];
  apps: AppSummary[];
  selectedAppKey?: string;
  onChanged: () => void;
}) {
  return selectedAppKey ? (
    <ClientAppInputSettingsDetail
      client={client}
      configs={configs}
      apps={apps}
      appKey={selectedAppKey}
      onChanged={onChanged}
    />
  ) : (
    <ClientInputSettingsSummary
      client={client}
      configs={configs}
      apps={apps}
      onChanged={onChanged}
    />
  );
}

function ClientInputSettingsSummary({
  client,
  configs,
  apps,
  onChanged,
}: {
  client: Client;
  configs: InputConfig[];
  apps: AppSummary[];
  onChanged: () => void;
}) {
  const [search, setSearch] = useState("");
  const [selectedApp, setSelectedApp] = useState("");
  const [editing, setEditing] = useState<EditingConfig | null>(null);
  const appsByKey = useMemo(() => new Map(apps.map((app) => [app.app_key, app])), [apps]);
  const groups = useMemo(
    () =>
      groupInputSettings(configs, (config) => config.app_key).sort((left, right) =>
        left.key.localeCompare(right.key),
      ),
    [configs],
  );
  const filteredGroups = useMemo(
    () =>
      groups.filter((group) =>
        inputSettingGroupMatches(group, search, [appsByKey.get(group.key)?.app_key || ""]),
      ),
    [appsByKey, groups, search],
  );

  useEffect(() => {
    if (!selectedApp && apps.length) setSelectedApp(apps[0]!.app_key);
  }, [apps, selectedApp]);

  function finish() {
    setEditing(null);
    onChanged();
  }

  return (
    <>
      <Panel
        title={translate("audit.inputSettings")}
        subtitle={translate("inputSettings.clientSubtitle")}
        actions={
          apps.length ? (
            <div className="inlineActions">
              <SelectControl
                value={selectedApp}
                ariaLabel={translate("inputSettings.appForNew")}
                onChange={setSelectedApp}
                options={apps.map((app) => ({ value: app.app_key, label: app.app_key }))}
              />
              <button
                className="button primary"
                type="button"
                disabled={!selectedApp}
                onClick={() => setEditing({ appKey: selectedApp })}
              >
                <Plus size={16} aria-hidden="true" />
                {translate("inputSettings.add")}
              </button>
            </div>
          ) : null
        }
      >
        {configs.length === 0 ? (
          <EmptyState
            title={
              apps.length
                ? translate("inputSettings.clientEmpty")
                : translate("inputSettings.noReleasedApps")
            }
          />
        ) : (
          <>
            <div className="settingsSummaryToolbar">
              <input
                className="searchInput"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={translate("inputSettings.searchClient")}
                aria-label={translate("inputSettings.searchClient")}
              />
              <span>
                {countLabel(
                  filteredGroups.length,
                  translate("inputSettings.configuredApp"),
                  translate("inputSettings.configuredApps"),
                )}
              </span>
            </div>
            {filteredGroups.length ? (
              <InputSettingSummaryTable
                id="clientInputSettingsSummary"
                scopeHeading={translate("apps.column.app")}
                rows={filteredGroups.map((group) => {
                  const app = appsByKey.get(group.key);
                  return {
                    group,
                    label: group.key,
                    subtitle: app
                      ? translate("inputSettings.releasedApp")
                      : translate("inputSettings.releaseUnavailable"),
                    href: `/clients/${client.id}/input-settings/${encodeURIComponent(group.key)}`,
                    coverage: countLabel(
                      group.configs.length,
                      translate("inputSettings.actionScopeLower"),
                      translate("inputSettings.actionScopes"),
                    ),
                    coverageDetail: group.actionKeys
                      .map((key) => key || translate("appDetail.allActions"))
                      .join(", "),
                  };
                })}
              />
            ) : (
              <EmptyState title={translate("inputSettings.noAppMatches")} />
            )}
          </>
        )}
      </Panel>

      {editing ? (
        <ClientInputConfigDialog
          client={client}
          appKey={editing.appKey}
          existing={editing.config}
          onClose={() => setEditing(null)}
          onSaved={finish}
        />
      ) : null}
    </>
  );
}

function ClientAppInputSettingsDetail({
  client,
  configs,
  apps,
  appKey,
  onChanged,
}: {
  client: Client;
  configs: InputConfig[];
  apps: AppSummary[];
  appKey: string;
  onChanged: () => void;
}) {
  const { api } = useApp();
  const [editing, setEditing] = useState<EditingConfig | null>(null);
  const app = apps.find((item) => item.app_key === appKey);
  const detail = useAsync(
    () => (app ? api.app(appKey) : Promise.resolve(null)),
    [api, app, appKey],
  );
  const scopedConfigs = useMemo(
    () => configs.filter((config) => config.app_key === appKey),
    [appKey, configs],
  );
  const actionsByKey = useMemo(
    () => new Map((detail.data?.actions || []).map((action) => [action.action_key, action])),
    [detail.data?.actions],
  );

  function finish() {
    setEditing(null);
    onChanged();
    detail.reload();
  }

  return (
    <>
      <Panel
        title={translate("inputSettings.namedSettings", { name: appKey })}
        subtitle={translate("inputSettings.clientAppSubtitle", { client: client.name })}
        actions={
          <>
            <Link className="button" to={`/clients/${client.id}/input-settings`}>
              {translate("inputSettings.backToApps")}
            </Link>
            {app ? (
              <button
                className="button primary"
                type="button"
                onClick={() => setEditing({ appKey })}
              >
                <Plus size={16} aria-hidden="true" />
                {translate("inputSettings.add")}
              </button>
            ) : null}
          </>
        }
      >
        {detail.error ? <ErrorNotice message={detail.error} onRetry={detail.reload} /> : null}
        {detail.loading && app && !detail.data ? <Loading /> : null}
        {!app ? (
          <div className="inlineNotice">
            {translate("inputSettings.releaseUnavailableReadOnly")}
          </div>
        ) : null}
        {scopedConfigs.length === 0 ? (
          <EmptyState title={translate("inputSettings.appClientEmpty")} />
        ) : null}
        {scopedConfigs.length ? (
          <InputSettingScopeList
            id="clientInputSettings"
            items={scopedConfigs.map((config) => {
              const action = config.action_key ? actionsByKey.get(config.action_key) : undefined;
              const actionName = action
                ? actionDisplayName(action.display_name) || action.action_key
                : config.action_key || translate("appDetail.allActions");
              return {
                key: `${config.app_key}-${config.action_key || "all"}`,
                config,
                primaryLabel: translate("apps.column.app"),
                primaryValue: app ? (
                  <Link to={`/apps/${app.git_source_id}/input-settings/client/${client.id}`}>
                    {appKey}
                  </Link>
                ) : (
                  appKey
                ),
                primaryMeta: app
                  ? translate("inputSettings.releasedApp")
                  : translate("inputSettings.releaseUnavailable"),
                actionName,
                actionMeta: config.action_key
                  ? translate("inputSettings.actionOverride", { action: config.action_key })
                  : translate("inputSettings.appWideOverride"),
                editLabel: translate("inputSettings.editAppAction", {
                  app: appKey,
                  action: actionName,
                }),
                editDisabled: !app,
                onEdit: () => setEditing({ appKey, config }),
              };
            })}
          />
        ) : null}
      </Panel>

      {editing ? (
        <ClientInputConfigDialog
          client={client}
          appKey={editing.appKey}
          existing={editing.config}
          onClose={() => setEditing(null)}
          onSaved={finish}
        />
      ) : null}
    </>
  );
}

function ClientInputConfigDialog({
  client,
  appKey,
  existing,
  onClose,
  onSaved,
}: {
  client: Client;
  appKey: string;
  existing?: InputConfig;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { api } = useApp();
  const state = useAsync(() => api.app(appKey), [api, appKey]);
  if (state.error) {
    return (
      <Modal title={translate("audit.inputSettings")} onClose={onClose}>
        <ErrorNotice message={state.error} onRetry={state.reload} />
      </Modal>
    );
  }
  if (!state.data)
    return (
      <Modal title={translate("audit.inputSettings")} onClose={onClose}>
        <Loading />
      </Modal>
    );
  return (
    <InputConfigDialog
      appKey={appKey}
      actions={state.data.actions}
      clients={[client]}
      existing={existing}
      fixedClientID={client.id}
      onClose={onClose}
      onSaved={onSaved}
    />
  );
}
