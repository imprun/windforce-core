import { useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { EmptyState, ErrorNotice, Loading, ReleaseStateBadge } from "../components/ui";
import { PublishReleaseDialog } from "../features/PublishReleaseDialog";
import { RegisterAppDialog } from "../features/RegisterAppDialog";
import type { AppSummary, GitSource } from "../lib/api";
import { useApp, useAsync } from "../lib/app-context";
import { formatRelative, shortSHA } from "../lib/format";
import { Link, useRouter } from "../lib/router";

type AppRow = {
  source: GitSource;
  app: AppSummary | null;
};

export function AppsPage() {
  const { api } = useApp();
  const { navigate } = useRouter();
  const [search, setSearch] = useState("");
  const [registering, setRegistering] = useState(false);
  const [publishing, setPublishing] = useState<GitSource | null>(null);

  const state = useAsync(
    async () => {
      const [sources, apps] = await Promise.all([api.gitSources(), api.apps()]);
      return { sources, apps: apps.apps || [] };
    },
    [api],
  );

  const rows = useMemo<AppRow[]>(() => {
    if (!state.data) return [];
    const bySource = new Map<number, AppSummary>();
    for (const app of state.data.apps) bySource.set(app.git_source_id, app);
    const query = search.trim().toLowerCase();
    return state.data.sources
      .map((source) => ({ source, app: bySource.get(source.id) || null }))
      .filter((row) => {
        if (!query) return true;
        return (
          row.source.name.toLowerCase().includes(query) ||
          row.source.repo_url.toLowerCase().includes(query) ||
          (row.app?.app_key || "").toLowerCase().includes(query)
        );
      });
  }, [state.data, search]);

  return (
    <Layout
      title="Apps"
      subtitle="Register apps, review repository sources, and publish worker-visible releases."
      actions={
        <>
          <input
            className="searchInput"
            placeholder="Filter apps…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            aria-label="Filter apps"
          />
          <button className="button" type="button" onClick={() => state.reload()}>
            Refresh
          </button>
          <button className="button primary" type="button" id="registerAppButton" onClick={() => setRegistering(true)}>
            Register App
          </button>
        </>
      }
    >
      {state.error ? <ErrorNotice message={state.error} onRetry={state.reload} /> : null}
      {state.loading && !state.data ? <Loading /> : null}

      {state.data ? (
        rows.length === 0 ? (
          <EmptyState title={search ? "No apps match the filter." : "No apps registered yet."}>
            {!search ? (
              <p>
                Register a repository source to create your first app, or create the managed sample app to explore the
                release flow.
              </p>
            ) : null}
          </EmptyState>
        ) : (
          <div className="tableWrap">
            <table className="table" id="appList">
              <thead>
                <tr>
                  <th>App</th>
                  <th>Release state</th>
                  <th>Repository source</th>
                  <th>Last release</th>
                  <th>Actions</th>
                  <th>Route tag</th>
                  <th aria-label="Row actions" />
                </tr>
              </thead>
              <tbody>
                {rows.map(({ source, app }) => (
                  <tr
                    key={source.id}
                    className="tableRow clickable"
                    onClick={() => navigate(`/apps/${source.id}`)}
                  >
                    <td>
                      <Link
                        to={`/apps/${source.id}`}
                        className="cellTitle"
                        onClick={(event) => event.stopPropagation()}
                      >
                        {source.name}
                      </Link>
                      <span className="cellSub">{app ? app.app_key : "not released"}</span>
                    </td>
                    <td>
                      <ReleaseStateBadge released={Boolean(app || source.last_synced_commit)} />
                    </td>
                    <td>
                      <span className="cellTitle mono">{repoLabel(source.repo_url)}</span>
                      <span className="cellSub mono">
                        {source.branch || "main"}
                        {source.subpath ? ` · ${source.subpath}` : ""}
                      </span>
                    </td>
                    <td>
                      <span className="cellTitle mono">{shortSHA(app?.commit_sha || source.last_synced_commit)}</span>
                      <span className="cellSub">{formatRelative(app?.updated_at || source.last_synced_at)}</span>
                    </td>
                    <td>{app ? app.actions_count : "—"}</td>
                    <td>{app ? <span className="mono">{app.effective_route_tag}</span> : "—"}</td>
                    <td className="rowActions" onClick={(event) => event.stopPropagation()}>
                      <button
                        className="button small primary"
                        type="button"
                        onClick={() => setPublishing(source)}
                      >
                        Publish Release
                      </button>
                      <Link className="button small" to={`/apps/${source.id}`}>
                        Open App
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
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

function repoLabel(repoURL: string): string {
  return repoURL.replace(/^https?:\/\//, "").replace(/\.git$/, "");
}
