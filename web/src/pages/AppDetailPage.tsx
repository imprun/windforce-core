import { RotateCcw } from "lucide-react";
import { useEffect, useState } from "react";
import { AppSettingsNav } from "../components/AppSettingsNav";
import { Layout } from "../components/Layout";
import { ReleaseMarkdown } from "../components/ReleaseMarkdown";
import { StatTile, WindowSelector, windowLabel } from "../components/stats";
import {
  DefinitionList,
  EmptyState,
  ErrorNotice,
  JsonBlock,
  Loading,
  Panel,
  ReleaseStateBadge,
} from "../components/ui";
import { AppInputSettings } from "../features/AppInputSettings";
import { AppRuntimeConfiguration } from "../features/AppRuntimeConfiguration";
import { AppTriggers } from "../features/AppTriggers";
import { AuditEventTable } from "../features/AuditEventTable";
import { ExecutionLimitsPanel } from "../features/ExecutionLimitsPanel";
import { ExecutionPlacementPanel } from "../features/ExecutionPlacementPanel";
import { PublishReleaseDialog } from "../features/PublishReleaseDialog";
import { RepositorySettings } from "../features/RepositorySettings";
import { RollbackReleaseDialog } from "../features/RollbackReleaseDialog";
import { SourceReleaseActions } from "../features/SourceReleaseActions";
import { actionDisplayName } from "../lib/action-label";
import type {
  ActionSchemas,
  ActionView,
  AppDetail,
  AppDocumentation,
  AppSummary,
  GitSource,
  HistoryItem,
} from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { findAppForSource } from "../lib/app-rows";
import {
  appSettingsPath,
  defaultAppSettingsTab,
  isAppSettingsTabKey,
} from "../lib/app-settings-navigation";
import { formatJSON, formatRelative, formatTime, shortSHA } from "../lib/format";
import { displayRepoURL, forgeCommitURL, forgeName, forgeTreeURL } from "../lib/repo";
import { Link, useRouter } from "../lib/router";
import { describeSchema, formatSchemaValue, type SchemaField } from "../lib/schema-document";
import { type TranslationKey, translate } from "../shared/i18n";

const tabs = [
  { key: "overview", labelKey: "appDetail.tab.overview" as TranslationKey },
  { key: "docs", labelKey: "appDetail.tab.docs" as TranslationKey },
  { key: "triggers", labelKey: "trigger.title" as TranslationKey },
  { key: "monitoring", labelKey: "navigation.monitoring" as TranslationKey },
  { key: "releases", labelKey: "appDetail.tab.releases" as TranslationKey },
  { key: "audit", labelKey: "navigation.audit" as TranslationKey },
  { key: "settings", labelKey: "appDetail.tab.settings" as TranslationKey },
] as const;

type TabKey = (typeof tabs)[number]["key"];

export function AppDetailPage({
  sourceID,
  tab,
  settingsTab,
  section,
  actionKey,
}: {
  sourceID: number;
  tab: string;
  settingsTab?: string;
  section?: string;
  actionKey?: string;
}) {
  const { api } = useApp();
  const { navigate } = useRouter();
  const [publishingSource, setPublishingSource] = useState<GitSource | null>(null);
  const [releaseHistoryRevision, setReleaseHistoryRevision] = useState(0);
  const [actionRevision, setActionRevision] = useState(0);

  const activeTab: TabKey = (tabs.find((item) => item.key === tab)?.key || "overview") as TabKey;

  const state = useAsync(async () => {
    const [sources, apps] = await Promise.all([api.gitSources(), api.apps()]);
    const source = sources.find((item) => item.id === sourceID) || null;
    const app = findAppForSource(source, apps.apps || [], sourceID);
    const detail = app ? await api.app(app.app_key) : null;
    return { source, app, detail };
  }, [api, sourceID]);

  const source = state.data?.source || null;
  const app = state.data?.app || null;
  const detail = state.data?.detail || null;
  const fallbackSettingsTab = defaultAppSettingsTab(Boolean(source));
  const activeSettingsTab =
    isAppSettingsTabKey(settingsTab) && (settingsTab !== "repository" || Boolean(source))
      ? settingsTab
      : fallbackSettingsTab;

  useEffect(() => {
    if (activeTab !== "settings" || !state.data || settingsTab === activeSettingsTab) return;
    navigate(appSettingsPath(sourceID, activeSettingsTab), { replace: true });
  }, [activeSettingsTab, activeTab, navigate, settingsTab, sourceID, state.data]);

  if (state.loading && !state.data) {
    return (
      <Layout title={translate("apps.column.app")}>
        <Loading />
      </Layout>
    );
  }

  if (state.error) {
    return (
      <Layout title={translate("apps.column.app")}>
        <ErrorNotice message={state.error} onRetry={state.reload} />
      </Layout>
    );
  }

  if (!source && !app) {
    return (
      <Layout title={translate("appDetail.notFound")}>
        <EmptyState title={translate("appDetail.notRegistered")}>
          <Link className="button" to="/">
            {translate("appDetail.backToApps")}
          </Link>
        </EmptyState>
      </Layout>
    );
  }

  const title = app?.app_key ?? source?.name ?? translate("apps.column.app");
  return (
    <Layout
      title={title}
      subtitle={
        source
          ? translate("appDetail.repositorySubtitle", {
              id: source.id,
              name: source.name !== title ? ` · ${source.name}` : "",
              repository: displayRepoURL(source.repo_url),
            })
          : translate("appDetail.repositoryRemovedActive")
      }
      actions={
        <>
          <ReleaseStateBadge released={Boolean(app)} bundleReady={app?.bundle_status === "ready"} />
          <button
            className="button"
            type="button"
            onClick={() => {
              setActionRevision((current) => current + 1);
              state.reload();
            }}
          >
            {translate("common.refresh")}
          </button>
          {source ? (
            <SourceReleaseActions
              key={`${source.id}:${actionRevision}`}
              source={source}
              activeCommit={app?.commit_sha}
              activeBundleReady={app?.bundle_status === "ready"}
              syncButtonID="syncSourceButton"
              publishButtonID="publishReleaseButton"
              onPublish={setPublishingSource}
            />
          ) : null}
        </>
      }
    >
      <nav className="tabBar" aria-label={translate("appDetail.tabs")}>
        {tabs.map((item) => (
          <Link
            key={item.key}
            className={item.key === activeTab ? "tab active" : "tab"}
            data-ui-guide={item.key === "settings" ? "app-settings" : undefined}
            to={
              item.key === "overview"
                ? `/apps/${sourceID}`
                : item.key === "settings"
                  ? appSettingsPath(sourceID, fallbackSettingsTab)
                  : `/apps/${sourceID}/${item.key}`
            }
          >
            {translate(item.labelKey)}
          </Link>
        ))}
      </nav>

      {activeTab === "settings" ? (
        <AppSettingsNav
          sourceID={sourceID}
          activeTab={activeSettingsTab}
          repositoryAvailable={Boolean(source)}
        />
      ) : null}

      {activeTab === "overview" ? (
        <OverviewTab sourceID={sourceID} source={source} app={app} detail={detail} />
      ) : null}
      {activeTab === "docs" ? (
        <DocsTab
          sourceID={sourceID}
          source={source}
          app={app}
          detail={detail}
          section={section}
          actionKey={actionKey}
        />
      ) : null}
      {activeTab === "triggers" && app && detail ? (
        <AppTriggers sourceID={sourceID} appKey={app.app_key} actions={detail.actions} />
      ) : null}
      {activeTab === "triggers" && (!app || !detail) ? (
        <Panel title={translate("trigger.title")} subtitle={translate("appDetail.triggerHint")}>
          <EmptyState title={translate("trigger.publishFirst")}>
            <p>{translate("trigger.needsAction")}</p>
          </EmptyState>
        </Panel>
      ) : null}
      {activeTab === "settings" && activeSettingsTab === "input-settings" && detail ? (
        <AppInputSettings
          detail={detail}
          sourceID={sourceID}
          selectedClientID={section === "client" ? actionKey : undefined}
        />
      ) : null}
      {activeTab === "settings" && activeSettingsTab === "runtime-config" && app ? (
        <AppRuntimeConfiguration appKey={app.app_key} />
      ) : null}
      {activeTab === "settings" && activeSettingsTab === "runtime-config" && !app ? (
        <Panel
          title={translate("appRuntime.configTitle")}
          subtitle={translate("appRuntime.publishFirstHint")}
        >
          <EmptyState title={translate("appRuntime.publishFirst")} />
        </Panel>
      ) : null}
      {activeTab === "settings" && activeSettingsTab === "placement" ? (
        <PlacementTab detail={detail} onUpdated={state.reload} />
      ) : null}
      {activeTab === "settings" && activeSettingsTab === "execution-limits" ? (
        <ExecutionLimitsTab detail={detail} />
      ) : null}
      {activeTab === "monitoring" ? <MonitoringTab app={app} /> : null}
      {activeTab === "settings" && activeSettingsTab === "repository" && source ? (
        <RepositorySettings source={source} onChanged={state.reload} />
      ) : null}
      {activeTab === "releases" ? (
        <ReleasesTab
          appKey={title}
          released={Boolean(app)}
          repoURL={source?.repo_url || ""}
          refreshRevision={releaseHistoryRevision}
          onRolledBack={() => {
            setReleaseHistoryRevision((current) => current + 1);
            setActionRevision((current) => current + 1);
            state.reload();
          }}
        />
      ) : null}
      {activeTab === "audit" ? (
        <AuditTab sourceID={sourceID} appKey={app?.app_key || source?.name || ""} />
      ) : null}
      {publishingSource ? (
        <PublishReleaseDialog
          source={publishingSource}
          appKey={app?.app_key}
          activeCommit={app?.commit_sha}
          onClose={() => setPublishingSource(null)}
          onPublished={() => {
            setPublishingSource(null);
            setReleaseHistoryRevision((current) => current + 1);
            state.reload();
            navigate(`/apps/${sourceID}/releases`);
          }}
        />
      ) : null}
    </Layout>
  );
}

function OverviewTab({
  sourceID,
  source,
  app,
  detail,
}: {
  sourceID: number;
  source: GitSource | null;
  app: AppSummary | null;
  detail: AppDetail | null;
}) {
  const { api } = useApp();
  const summary = useAsync(() => api.jobsSummary(), [api]);

  if (!app || !detail) {
    return (
      <Panel
        title={translate("appDetail.activeContract")}
        subtitle={translate("appDetail.activeContractHint")}
      >
        <EmptyState title={translate("appDetail.noRelease")}>
          <p>{translate("appDetail.noReleaseHint")}</p>
        </EmptyState>
      </Panel>
    );
  }

  const effectiveWorkerTag = app.effective_route_tag || app.tag;
  const tagSummary = summary.data?.by_tag?.find((item) => item.tag === effectiveWorkerTag);
  const tagActivity = summary.error
    ? translate("appDetail.unavailable")
    : summary.loading
      ? translate("settings.health.checking")
      : tagSummary
        ? translate("appDetail.tagActivity", {
            queued: tagSummary.queued_count,
            running: tagSummary.running_count,
            completed: tagSummary.completed_count_recent,
          })
        : translate("appDetail.noRecentTagJobs");

  return (
    <>
      <Panel
        title={translate("release.active")}
        subtitle={translate("appDetail.activeReleaseHint")}
      >
        <div className="releaseSummary">
          <div className="releaseIdentity">
            <p className="eyebrow">{translate("appDetail.releaseCommit")}</p>
            <p className="releaseCommit">
              <CommitRef repoURL={source?.repo_url || ""} commit={app.commit_sha} />
            </p>
            <p className="cellSub">
              {translate("appDetail.updatedRelative", { time: formatRelative(app.updated_at) })}
            </p>
          </div>
          <DefinitionList
            className="overviewFacts"
            items={[
              [
                translate("appDetail.sourceCode"),
                <SourceCodeRef
                  repoURL={source?.repo_url || ""}
                  commit={app.commit_sha}
                  subpath={source?.subpath || ""}
                />,
              ],
              [translate("appDetail.entrypoint"), <span className="mono">{app.entrypoint}</span>],
              [translate("appDetail.scriptLanguage"), app.script_lang],
              [
                translate("appDetail.executionBundle"),
                app.bundle_status === "ready" && app.bundle_digest ? (
                  <span>
                    <strong>{translate("info.ready")}</strong> ·{" "}
                    <span className="mono">
                      {shortSHA(app.bundle_digest.replace(/^sha256:/, ""), 12)}
                    </span>
                  </span>
                ) : (
                  translate("appDetail.bundleMissing")
                ),
              ],
              [translate("appDetail.releaseWorkerTag"), <span className="mono">{app.tag}</span>],
              [
                translate("appDetail.execution"),
                `${app.timeout_s}s${app.required_capabilities?.length ? ` · ${app.required_capabilities.join(", ")}` : ""}`,
              ],
              [
                translate("appDetail.apiReference"),
                <Link to={`/apps/${sourceID}/docs/reference`}>
                  {translate("appDetail.actionCount", { count: detail.actions.length })}
                </Link>,
              ],
            ]}
          />
        </div>
      </Panel>

      <Panel title={translate("info.readiness")} subtitle={translate("appDetail.readinessHint")}>
        <DefinitionList
          className="readinessFacts"
          items={[
            [
              translate("release.registered"),
              source ? formatTime(source.created_at) : translate("apps.repositorySourceRemoved"),
            ],
            [
              translate("appDetail.workerArtifact"),
              app.bundle_status === "ready" ? translate("info.ready") : translate("info.notReady"),
            ],
            [
              translate("apps.column.lastRelease"),
              `${formatTime(app.updated_at)} (${formatRelative(app.updated_at)})`,
            ],
            [
              translate("appDetail.latestSynchronizedSource"),
              source?.last_synced_commit
                ? `${shortSHA(source.last_synced_commit, 12)} · ${formatRelative(source.last_synced_at)}`
                : translate("appDetail.notSynchronized"),
            ],
            [
              translate("appDetail.effectiveWorkerTag"),
              <Link to={appSettingsPath(sourceID, "placement")}>
                <span className="mono">{effectiveWorkerTag}</span>
              </Link>,
            ],
            [translate("appDetail.jobsOnWorkerTag", { tag: effectiveWorkerTag }), tagActivity],
          ]}
        />
      </Panel>
    </>
  );
}

function PlacementTab({ detail, onUpdated }: { detail: AppDetail | null; onUpdated: () => void }) {
  if (!detail) {
    return (
      <Panel title={translate("routing.title")} subtitle={translate("routing.subtitle")}>
        <EmptyState title={translate("appDetail.noRelease")}>
          <p>{translate("appDetail.placementNeedsRelease")}</p>
        </EmptyState>
      </Panel>
    );
  }
  return <ExecutionPlacementPanel detail={detail} onUpdated={onUpdated} />;
}

function ExecutionLimitsTab({ detail }: { detail: AppDetail | null }) {
  if (!detail) {
    return (
      <Panel
        title={translate("executionLimits.title")}
        subtitle={translate("executionLimits.subtitle")}
      >
        <EmptyState title={translate("appDetail.noRelease")}>
          <p>{translate("executionLimits.needsRelease")}</p>
        </EmptyState>
      </Panel>
    );
  }
  return <ExecutionLimitsPanel detail={detail} />;
}

function ReleasesTab({
  appKey,
  released,
  repoURL,
  refreshRevision,
  onRolledBack,
}: {
  appKey: string;
  released: boolean;
  repoURL: string;
  refreshRevision: number;
  onRolledBack: () => void;
}) {
  const { api } = useApp();
  const [rollbackTarget, setRollbackTarget] = useState<HistoryItem | null>(null);
  const state = useAsync(
    async () => (released ? api.appHistory(appKey) : Promise.resolve([])),
    [api, appKey, released, refreshRevision],
  );
  const activeRelease = state.data?.find((item) => item.active) || null;

  return (
    <Panel
      title={translate("appDetail.releaseHistory")}
      subtitle={translate("appDetail.releaseHistoryHint")}
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading ? <Loading /> : null}
      {state.data && state.data.length === 0 ? (
        <EmptyState title={translate("appDetail.noReleaseHistory")} />
      ) : null}
      {state.data && state.data.length > 0 ? (
        <div className="tableWrap">
          <table className="table" id="releaseHistory">
            <thead>
              <tr>
                <th>{translate("common.status")}</th>
                <th>{translate("trigger.delivery.when")}</th>
                <th>{translate("settings.actor")}</th>
                <th>{translate("appDetail.commit")}</th>
                <th>{translate("appDetail.source")}</th>
                <th title={translate("appDetail.releaseIDHint")}>
                  {translate("appDetail.releaseID")}
                </th>
                <th>{translate("appDetail.note")}</th>
                <th>{translate("trigger.column.action")}</th>
              </tr>
            </thead>
            <tbody>
              {state.data.map((item) => (
                <tr key={item.id} className={item.active ? "activeReleaseRow" : undefined}>
                  <td>
                    {item.active ? (
                      <span
                        className={`badge ${item.bundle_status === "ready" ? "badge-good" : "badge-warning"}`}
                      >
                        {item.bundle_status === "ready"
                          ? translate("workspace.status.active")
                          : translate("appDetail.activeBundleMissing")}
                      </span>
                    ) : item.bundle_status === "ready" ? (
                      <span className="badge badge-neutral">
                        {translate("appDetail.historical")}
                      </span>
                    ) : (
                      <span className="badge badge-warning">
                        {translate("appDetail.bundleMissingShort")}
                      </span>
                    )}
                  </td>
                  <td>
                    <span className="cellTitle">{formatRelative(item.created_at)}</span>
                    <span className="cellSub">{formatTime(item.created_at)}</span>
                  </td>
                  <td>{item.created_by || "system"}</td>
                  <td>
                    <CommitRef repoURL={repoURL} commit={item.commit_sha} />
                  </td>
                  <td>{item.source}</td>
                  <td className="mono">
                    <span title={translate("appDetail.releaseIDNamed", { id: item.id })}>
                      {shortSHA(item.id, 12)}
                    </span>
                  </td>
                  <td>{item.message || "—"}</td>
                  <td>
                    {!item.active && item.bundle_status === "ready" ? (
                      <button
                        className="button small"
                        type="button"
                        onClick={() => setRollbackTarget(item)}
                      >
                        <RotateCcw size={15} aria-hidden="true" />
                        {translate("release.rollback")}
                      </button>
                    ) : (
                      <span className="cellSub">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {rollbackTarget ? (
        <RollbackReleaseDialog
          appKey={appKey}
          target={rollbackTarget}
          active={activeRelease}
          onClose={() => setRollbackTarget(null)}
          onRolledBack={() => {
            setRollbackTarget(null);
            state.reload();
            onRolledBack();
          }}
        />
      ) : null}
    </Panel>
  );
}

function DocsTab({
  sourceID,
  source,
  app,
  detail,
  section,
  actionKey,
}: {
  sourceID: number;
  source: GitSource | null;
  app: AppSummary | null;
  detail: AppDetail | null;
  section?: string;
  actionKey?: string;
}) {
  if (!app || !detail) {
    return (
      <Panel
        title={translate("appDetail.documentation")}
        subtitle={translate("appDetail.documentationHint")}
      >
        <EmptyState title={translate("appDetail.noRelease")}>
          <p>{translate("appDetail.publishForDocs")}</p>
        </EmptyState>
      </Panel>
    );
  }

  const activeSection = section === "actions" ? section : "guide";
  const actions = sortActions(detail.actions);
  const selectedAction =
    activeSection === "actions"
      ? actions.find((item) => item.action_key === actionKey) || null
      : null;
  return (
    <Panel
      title={translate("appDetail.documentation")}
      subtitle={translate("appDetail.documentationHint")}
    >
      <div className="docsLayout">
        <aside className="docsNav" aria-label={translate("appDetail.docsNavigation")}>
          <p className="docsNavTitle">{translate("appDetail.tab.docs")}</p>
          <Link
            className={activeSection === "guide" ? "docsNavLink active" : "docsNavLink"}
            to={`/apps/${sourceID}/docs`}
          >
            {translate("appDetail.guide")}
          </Link>
          <p className="docsNavTitle">{translate("common.actions")}</p>
          <Link
            className={
              activeSection === "actions" && !actionKey ? "docsNavLink active" : "docsNavLink"
            }
            to={`/apps/${sourceID}/docs/actions`}
          >
            {translate("appDetail.allActions")}
          </Link>
          {actions.map((action) => (
            <Link
              key={action.action_key}
              className={
                action.action_key === actionKey
                  ? "docsNavLink docsNavAction active"
                  : "docsNavLink docsNavAction"
              }
              to={`/apps/${sourceID}/docs/actions/${encodeURIComponent(action.action_key)}`}
            >
              <ActionLabel action={action} />
            </Link>
          ))}
        </aside>
        <section className="docsMain">
          {activeSection === "guide" ? <GuideDocument source={source} app={app} /> : null}
          {activeSection === "actions" && !actionKey ? (
            <ActionReferenceList sourceID={sourceID} actions={actions} />
          ) : null}
          {activeSection === "actions" && selectedAction ? (
            <ActionReferenceDetail app={app} action={selectedAction} />
          ) : null}
          {activeSection === "actions" && !selectedAction ? (
            <EmptyState title={translate("appDetail.actionNotFound")} />
          ) : null}
        </section>
      </div>
    </Panel>
  );
}

function GuideDocument({ source, app }: { source: GitSource | null; app: AppSummary }) {
  const { api } = useApp();
  const documentation = useAsync(() => api.appDocumentation(app.app_key), [api, app.app_key]);
  if (documentation.loading && !documentation.data) return <Loading />;
  if (documentation.error)
    return <ErrorNotice message={documentation.error} onRetry={documentation.reload} />;
  return <RenderedGuide documentation={documentation.data} source={source} />;
}

function RenderedGuide({
  documentation,
  source,
}: {
  documentation: AppDocumentation | null;
  source: GitSource | null;
}) {
  if (!documentation?.available || !documentation.markdown) {
    return (
      <EmptyState title={translate("appDetail.noReadme")}>
        <p>{translate("appDetail.noReadmeHint")}</p>
      </EmptyState>
    );
  }
  return (
    <article className="docsArticle">
      <header className="docsHeader">
        <h2>{translate("appDetail.guide")}</h2>
        <p>
          {translate("appDetail.pinnedToRelease", {
            path: documentation.path || "README.md",
            commit: shortSHA(documentation.commit_sha, 12),
          })}
        </p>
      </header>
      <ReleaseMarkdown
        markdown={documentation.markdown}
        repoURL={source?.repo_url || ""}
        commit={documentation.commit_sha}
        subpath={source?.subpath || ""}
      />
    </article>
  );
}

function ActionReferenceList({ sourceID, actions }: { sourceID: number; actions: ActionView[] }) {
  return (
    <section className="docsArticle">
      <header className="docsHeader">
        <div className="docsHeaderRow">
          <div>
            <h2>{translate("common.actions")}</h2>
            <p>{translate("appDetail.selectAction")}</p>
          </div>
        </div>
      </header>
      {actions.length === 0 ? (
        <EmptyState title={translate("appDetail.noActions")} />
      ) : (
        <div className="docsActionList">
          {actions.map((action) => (
            <Link
              key={action.action_key}
              className="docsActionRow"
              to={`/apps/${sourceID}/docs/actions/${encodeURIComponent(action.action_key)}`}
            >
              <ActionLabel action={action} />
            </Link>
          ))}
        </div>
      )}
    </section>
  );
}

function ActionReferenceDetail({ app, action }: { app: AppSummary; action: ActionView }) {
  const { api } = useApp();
  const schemas = useAsync(
    () => api.actionSchemas(app.app_key, action.action_key),
    [api, app.app_key, action.action_key],
  );
  const name = actionDisplayName(action.display_name);
  return (
    <article className="docsArticle">
      <header className="docsHeader">
        <div className="docsHeaderRow">
          <div>
            <h2>{name || translate("appDetail.actionNamed", { action: action.action_key })}</h2>
            <p>
              {translate("appDetail.actionKey")} <span className="mono">{action.action_key}</span>
            </p>
          </div>
        </div>
      </header>
      <RuntimeAccessSummary action={action} />
      {schemas.error ? <ErrorNotice message={schemas.error} onRetry={schemas.reload} /> : null}
      <SchemaReference
        schemas={schemas.data}
        loading={schemas.loading}
        appKey={app.app_key}
        actionKey={action.action_key}
      />
    </article>
  );
}

function RuntimeAccessSummary({ action }: { action: ActionView }) {
  const variables = action.runtime_access?.variables || [];
  const resources = action.runtime_access?.resources || [];
  return (
    <section className="runtimeAccessSummary">
      <div className="runtimeAccessSummaryHeader">
        <div>
          <h3>{translate("appDetail.runtimeAccess")}</h3>
          <p>{translate("appDetail.runtimeAccessDescription")}</p>
        </div>
        <Link className="button small" to="/settings/variables">
          {translate("appDetail.manageRuntimeConfiguration")}
        </Link>
      </div>
      {variables.length || resources.length ? (
        <div className="runtimeAccessGroups">
          {variables.length ? (
            <div>
              <span className="cellSub">{translate("runtimeConfig.tab.variables")}</span>
              <div className="runtimeAccessPaths">
                {variables.map((path) => (
                  <code className="badge neutral" key={`variable-${path}`}>
                    $var:{path}
                  </code>
                ))}
              </div>
            </div>
          ) : null}
          {resources.length ? (
            <div>
              <span className="cellSub">{translate("runtimeConfig.tab.resources")}</span>
              <div className="runtimeAccessPaths">
                {resources.map((path) => (
                  <code className="badge neutral" key={`resource-${path}`}>
                    $res:{path}
                  </code>
                ))}
              </div>
            </div>
          ) : null}
        </div>
      ) : (
        <p className="runtimeAccessEmpty">{translate("appDetail.runtimeAccessEmpty")}</p>
      )}
    </section>
  );
}

function ActionLabel({ action }: { action: ActionView }) {
  const displayName = actionDisplayName(action.display_name);
  return (
    <span className="actionLabel">
      <span className="actionLabelName">
        {displayName || translate("appDetail.actionNamed", { action: action.action_key })}
      </span>
      <span className="actionLabelKey mono">
        {translate("appDetail.actionKeyNamed", { action: action.action_key })}
      </span>
    </span>
  );
}

function sortActions(actions: ActionView[]): ActionView[] {
  return [...actions].sort((left, right) => compareActionKeys(left.action_key, right.action_key));
}

function compareActionKeys(left: string, right: string): number {
  const numeric = /^\d+$/;
  const leftNumeric = numeric.test(left);
  const rightNumeric = numeric.test(right);
  if (leftNumeric && rightNumeric) {
    const normalizedLeft = left.replace(/^0+/, "") || "0";
    const normalizedRight = right.replace(/^0+/, "") || "0";
    if (normalizedLeft.length !== normalizedRight.length)
      return normalizedLeft.length - normalizedRight.length;
    return normalizedLeft < normalizedRight
      ? -1
      : normalizedLeft > normalizedRight
        ? 1
        : left.localeCompare(right);
  }
  if (leftNumeric !== rightNumeric) return leftNumeric ? -1 : 1;
  return left.localeCompare(right);
}

function SchemaReference({
  schemas,
  loading,
  appKey,
  actionKey,
}: {
  schemas: ActionSchemas | null;
  loading: boolean;
  appKey: string;
  actionKey: string;
}) {
  if (loading && !schemas) return <Loading />;
  if (!schemas) return null;
  return (
    <div className="schemaStack">
      <SchemaSection
        title={translate("appDetail.requestBody")}
        emptyMessage={translate("appDetail.requestSchemaEmpty")}
        exampleLabel={translate("appDetail.exampleRequest")}
        filename={schemaFilename(appKey, actionKey, "input")}
        schema={schemas.input_schema}
      />
      <SchemaSection
        title={translate("appDetail.resultPayload")}
        emptyMessage={translate("appDetail.resultSchemaEmpty")}
        exampleLabel={translate("appDetail.exampleResult")}
        filename={schemaFilename(appKey, actionKey, "output")}
        schema={schemas.output_schema}
      />
    </div>
  );
}

function SchemaSection({
  title,
  emptyMessage,
  exampleLabel,
  filename,
  schema,
}: {
  title: string;
  emptyMessage: string;
  exampleLabel: string;
  filename: string;
  schema: unknown;
}) {
  const document = describeSchema(schema);
  return (
    <section className="schemaSection">
      <header className="schemaSectionHeader">
        <div>
          <h3>{title}</h3>
        </div>
        <div className="schemaSectionActions">
          <span className="schemaType mono">{document.type}</span>
          <SchemaArtifactControls filename={filename} schema={schema} />
        </div>
      </header>
      {document.fields.length > 0 ? (
        <SchemaFieldTable fields={document.fields} />
      ) : (
        <p className="schemaEmpty">{emptyMessage}</p>
      )}
      <div className="schemaExample">
        <div className="schemaExampleHeader">
          <h4>{exampleLabel}</h4>
          <span className="cellSub">
            {document.example.source === "declared"
              ? translate("appDetail.declaredInSchema")
              : translate("appDetail.generatedFromSchema")}
          </span>
        </div>
        <JsonBlock value={formatJSON(document.example.value)} maxHeight={360} />
      </div>
      <details className="schemaSource">
        <summary>{translate("appDetail.rawSchema")}</summary>
        <JsonBlock value={formatJSON(schema)} maxHeight={480} />
      </details>
    </section>
  );
}

function SchemaArtifactControls({ filename, schema }: { filename: string; schema: unknown }) {
  const { notify } = useApp();
  const [copied, setCopied] = useState(false);
  const text = formatJSON(schema);

  const handleCopy = async () => {
    try {
      await copyText(text);
      setCopied(true);
      notify("ok", translate("appDetail.schemaCopied"));
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      notify("error", translate("appDetail.schemaCopyFailed"));
    }
  };

  const handleDownload = () => {
    const blob = new Blob([text], { type: "application/schema+json;charset=utf-8" });
    const objectURL = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = objectURL;
    link.download = filename;
    document.body.append(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(objectURL), 0);
  };

  return (
    <div className="schemaArtifactControls">
      <button className="button small" type="button" onClick={() => void handleCopy()}>
        {copied ? translate("appDetail.copied") : translate("appDetail.copyJSON")}
      </button>
      <button className="button small" type="button" onClick={handleDownload}>
        {translate("appDetail.downloadJSON")}
      </button>
    </div>
  );
}

async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textArea = document.createElement("textarea");
  textArea.value = text;
  textArea.setAttribute("readonly", "");
  textArea.style.position = "fixed";
  textArea.style.opacity = "0";
  document.body.append(textArea);
  textArea.select();
  const copied = document.execCommand("copy");
  textArea.remove();
  if (!copied) throw new Error("clipboard unavailable");
}

function schemaFilename(appKey: string, actionKey: string, kind: "input" | "output"): string {
  const part = (value: string) => value.replace(/[^a-z0-9._-]+/giu, "_");
  return `${part(appKey)}-${part(actionKey)}-${kind}.schema.json`;
}

function SchemaFieldTable({ fields }: { fields: SchemaField[] }) {
  return (
    <div className="tableWrap schemaTableWrap">
      <table className="table schemaTable">
        <thead>
          <tr>
            <th>{translate("appDetail.field")}</th>
            <th>{translate("appDetail.description")}</th>
            <th>{translate("appDetail.constraints")}</th>
          </tr>
        </thead>
        <tbody>
          {fields.map((field) => (
            <tr key={field.name}>
              <td>
                {field.title ? <span className="cellTitle">{field.title}</span> : null}
                <div className="schemaFieldIdentity">
                  <span className="mono">{field.name}</span>
                  <span className="schemaFieldType mono">
                    {field.format ? `${field.type} (${field.format})` : field.type}
                  </span>
                  {field.required ? (
                    <span className="badge badge-good">{translate("appDetail.required")}</span>
                  ) : (
                    <span className="cellSub">{translate("appDetail.optional")}</span>
                  )}
                </div>
              </td>
              <td>{field.description || "—"}</td>
              <td>
                <SchemaFieldValues field={field} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function SchemaFieldValues({ field }: { field: SchemaField }) {
  const values: Array<[string, unknown]> = [];
  if (field.constValue !== undefined) values.push([translate("appDetail.fixed"), field.constValue]);
  if (field.enumValues?.length) values.push([translate("appDetail.allowed"), field.enumValues]);
  if (field.hasDefault) values.push([translate("appDetail.default"), field.defaultValue]);
  if (values.length === 0) return <span>—</span>;
  return (
    <div className="schemaFieldValues">
      {values.map(([label, value]) => (
        <span key={label}>
          <span className="schemaValueLabel">{label}</span>{" "}
          <span className="mono">{formatSchemaValue(value)}</span>
        </span>
      ))}
    </div>
  );
}

// Commit reference: linked to the forge commit page when the repo host is
// GitHub/GitLab, plain text otherwise.
function CommitRef({ repoURL, commit }: { repoURL: string; commit: string | null | undefined }) {
  if (!commit) return <span>—</span>;
  const url = forgeCommitURL(repoURL, commit);
  if (!url) return <span className="mono">{shortSHA(commit, 12)}</span>;
  return (
    <a className="mono" href={url} target="_blank" rel="noreferrer">
      {shortSHA(commit, 12)}
    </a>
  );
}

// The UI does not mirror app source; it links to the repository host at the
// pinned release commit (ADR 0006).
function SourceCodeRef({
  repoURL,
  commit,
  subpath,
}: {
  repoURL: string;
  commit: string | null | undefined;
  subpath: string;
}) {
  const url = forgeTreeURL(repoURL, commit, subpath);
  if (url) {
    return (
      <a href={url} target="_blank" rel="noreferrer">
        {translate("appDetail.browseSource", {
          path: subpath || translate("audit.repository"),
          commit: shortSHA(commit, 10),
          forge: forgeName(repoURL),
        })}
      </a>
    );
  }
  if (!repoURL) return <span>{translate("apps.repositorySourceRemoved")}</span>;
  return (
    <span className="mono">
      {displayRepoURL(repoURL)}
      {subpath ? ` · ${subpath}` : ""}
    </span>
  );
}

// Per-app slice of the workspace job aggregates (ADR 0005): the same
// summary endpoint, narrowed to this app's activity.
function MonitoringTab({ app }: { app: AppSummary | null }) {
  const { api } = useApp();
  const [windowSeconds, setWindowSeconds] = useState<number>(86400);
  const summary = useAsync(() => api.jobsSummary(windowSeconds), [api, windowSeconds]);

  if (!app) {
    return (
      <Panel
        title={translate("navigation.monitoring")}
        subtitle={translate("appDetail.monitoringHint")}
      >
        <EmptyState title={translate("appDetail.noReleaseActivity")} />
      </Panel>
    );
  }

  const counts = summary.data?.by_app?.find((item) => item.app_key === app.app_key);
  const label = windowLabel(windowSeconds);
  const settled = counts ? counts.completed_count_recent + counts.failed_count_recent : 0;
  const failurePercent =
    counts && settled > 0 ? (counts.failed_count_recent / settled) * 100 : null;
  const failureRate =
    failurePercent === null
      ? "—"
      : `${failurePercent.toFixed(failurePercent > 0 && failurePercent < 1 ? 1 : 0)}%`;

  return (
    <Panel
      title={translate("navigation.monitoring")}
      subtitle={translate("appDetail.monitoringNamedHint", { app: app.app_key })}
      actions={<WindowSelector value={windowSeconds} onChange={setWindowSeconds} />}
    >
      {summary.error ? <ErrorNotice message={summary.error} onRetry={summary.reload} /> : null}
      {summary.loading && !summary.data ? <Loading /> : null}
      {summary.data ? (
        <div className="statRow" id="appMonitoring">
          <StatTile
            label={translate("monitoring.queued")}
            value={counts?.queued_count ?? 0}
            tone="waiting"
          />
          <StatTile
            label={translate("monitoring.running")}
            value={counts?.running_count ?? 0}
            tone="running"
          />
          <StatTile
            label={translate("monitoring.completedWindow", { window: label })}
            value={counts?.completed_count_recent ?? 0}
            tone="good"
          />
          <StatTile
            label={translate("monitoring.failedWindow", { window: label })}
            value={counts?.failed_count_recent ?? 0}
            tone="critical"
          />
          <StatTile
            label={translate("monitoring.canceledWindow", { window: label })}
            value={counts?.canceled_count_recent ?? 0}
            tone="serious"
          />
          <StatTile
            label={translate("appDetail.failureRateWindow", { window: label })}
            value={failureRate}
            tone="neutral"
          />
        </div>
      ) : null}
      {summary.data && !counts ? (
        <p className="cellSub">{translate("appDetail.noActivityWindow")}</p>
      ) : null}
    </Panel>
  );
}

function AuditTab({ sourceID, appKey }: { sourceID: number; appKey: string }) {
  const { api } = useApp();
  const state = useAsync(
    () => api.auditEvents({ appKey, gitSourceID: sourceID, limit: 250 }),
    [api, sourceID, appKey],
  );

  return (
    <Panel
      title={translate("appDetail.auditTrail")}
      subtitle={translate("appDetail.auditTrailHint")}
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading /> : null}
      {state.data ? (
        <AuditEventTable events={state.data} emptyTitle={translate("appDetail.noChanges")} />
      ) : null}
    </Panel>
  );
}
