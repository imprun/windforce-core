import { useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { EmptyState, ErrorNotice, Loading, ReleaseStateBadge } from "../components/ui";
import { PublishReleaseDialog } from "../features/PublishReleaseDialog";
import { RegisterAppDialog } from "../features/RegisterAppDialog";
import { SourceReleaseActions } from "../features/SourceReleaseActions";
import type { GitSource } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, shortSHA } from "../lib/format";
import { displayRepoURL } from "../lib/repo";
import { Link, useRouter } from "../lib/router";
import { translate } from "../shared/i18n";
import { buildAppRows } from "../lib/app-rows";

export function AppsPage() {
  const { api } = useApp();
  const { navigate } = useRouter();
  const [search, setSearch] = useState("");
  const [registering, setRegistering] = useState(false);
  const [publishing, setPublishing] = useState<GitSource | null>(null);
  const [actionRevision, setActionRevision] = useState(0);

  const state = useAsync(async () => {
    const [sources, apps] = await Promise.all([api.gitSources(), api.apps()]);
    return { sources, apps: apps.apps || [] };
  }, [api]);

  const rows = useMemo(() => {
    if (!state.data) return [];
    const query = search.trim().toLowerCase();
    return buildAppRows(state.data.sources, state.data.apps).filter((row) => {
      if (!query) return true;
      return (
        (row.source?.name || "").toLowerCase().includes(query) ||
        (row.source?.repo_url || "").toLowerCase().includes(query) ||
        (row.app?.app_key || "").toLowerCase().includes(query)
      );
    });
  }, [state.data, search]);

  return (
    <Layout
      title={translate("apps.title")}
      subtitle={translate("apps.subtitle")}
      actions={
        <>
          <input
            className="searchInput"
            placeholder={translate("apps.filter")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            aria-label={translate("apps.filter")}
          />
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
          <button
            className="button primary"
            type="button"
            id="registerAppButton"
            onClick={() => setRegistering(true)}
          >
            {translate("apps.register")}
          </button>
        </>
      }
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading /> : null}

      {state.data ? (
        <>
          <section
            className="grid gap-3 sm:grid-cols-3"
            aria-label={translate("apps.workspaceSummary")}
          >
            {[
              {
                label: translate("apps.repositorySources"),
                value: state.data.sources.length,
                detail: translate("apps.registeredInWorkspace"),
              },
              {
                label: translate("apps.publishedApps"),
                value: state.data.apps.length,
                detail: translate("apps.availableToWorkers"),
              },
              {
                label: translate("common.actions"),
                value: state.data.apps.reduce((total, app) => total + app.actions_count, 0),
                detail: translate("apps.inActiveReleases"),
              },
            ].map((item) => (
              <article
                key={item.label}
                className="min-h-32 rounded-lg border border-border bg-surface p-4"
              >
                <span className="text-xs text-muted-foreground">{item.label}</span>
                <strong className="mt-4 block text-3xl font-semibold tracking-tight">
                  {item.value}
                </strong>
                <span className="mt-1 block text-xs text-muted-foreground">{item.detail}</span>
              </article>
            ))}
          </section>
          {rows.length === 0 ? (
            <EmptyState title={search ? translate("apps.noMatches") : translate("apps.empty")}>
              {!search ? <p>{translate("apps.emptyHint")}</p> : null}
            </EmptyState>
          ) : (
            <div className="tableWrap">
              <table className="table" id="appList">
                <thead>
                  <tr>
                    <th>{translate("apps.column.app")}</th>
                    <th>{translate("apps.column.releaseState")}</th>
                    <th>{translate("apps.column.repositorySource")}</th>
                    <th>{translate("apps.column.lastRelease")}</th>
                    <th>{translate("common.actions")}</th>
                    <th>{translate("apps.column.routeTag")}</th>
                    <th aria-label={translate("common.rowActions")} />
                  </tr>
                </thead>
                <tbody>
                  {rows.map(({ source, app }) => {
                    const detailID = source ? source.id : app?.git_source_id;
                    return (
                      <tr key={detailID} className="tableRow">
                        <td>
                          <Link to={`/apps/${detailID}`} className="cellTitle">
                            {app ? app.app_key : source?.name}
                          </Link>
                          <span className="cellSub">
                            {app
                              ? source
                                ? source.name !== app.app_key
                                  ? translate("apps.sourceNamed", { name: source.name })
                                  : translate("release.released")
                                : translate("apps.repositorySourceRemoved")
                              : translate("apps.registeredPendingRelease")}
                          </span>
                        </td>
                        <td>
                          <ReleaseStateBadge
                            released={Boolean(app)}
                            bundleReady={app?.bundle_status === "ready"}
                          />
                        </td>
                        <td>
                          {source ? (
                            <>
                              <span className="cellTitle mono">
                                {displayRepoURL(source.repo_url)}
                              </span>
                              <span className="cellSub mono">
                                {source.branch || "main"}
                                {source.subpath ? ` · ${source.subpath}` : ""}
                                {source.last_synced_commit
                                  ? translate("apps.syncedCommit", {
                                      commit: shortSHA(source.last_synced_commit, 8),
                                    })
                                  : translate("apps.notSynced")}
                              </span>
                            </>
                          ) : (
                            <span className="cellSub">
                              {translate("apps.repositorySourceRemoved")}
                            </span>
                          )}
                        </td>
                        <td>
                          <span className="cellTitle mono">{shortSHA(app?.commit_sha)}</span>
                          <span className="cellSub">{formatRelative(app?.updated_at)}</span>
                        </td>
                        <td>{app ? app.actions_count : "—"}</td>
                        <td>
                          {app ? <span className="mono">{app.effective_route_tag}</span> : "—"}
                        </td>
                        <td className="rowActions">
                          {source ? (
                            <SourceReleaseActions
                              key={`${source.id}:${actionRevision}`}
                              compact
                              source={source}
                              activeCommit={app?.commit_sha}
                              activeBundleReady={app?.bundle_status === "ready"}
                              onPublish={setPublishing}
                            />
                          ) : null}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      ) : null}

      {registering ? (
        <RegisterAppDialog
          onClose={() => setRegistering(false)}
          onRegistered={(created) => {
            setRegistering(false);
            state.reload();
            navigate(`/apps/${created.id}`);
          }}
        />
      ) : null}
      {publishing ? (
        <PublishReleaseDialog
          source={publishing}
          activeCommit={
            state.data?.apps.find(
              (app) =>
                (publishing.app_key && app.app_key === publishing.app_key) ||
                app.git_source_id === publishing.id,
            )?.commit_sha
          }
          onClose={() => setPublishing(null)}
          onPublished={() => {
            const id = publishing.id;
            setPublishing(null);
            state.reload();
            navigate(`/apps/${id}/releases`);
          }}
        />
      ) : null}
    </Layout>
  );
}
