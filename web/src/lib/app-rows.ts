import type { AppSummary, GitSource } from "./api";

// Either side may be missing: a registered source may not be released yet,
// and a released app's source registration may have been deleted.
export type AppRow = {
  source: GitSource | null;
  app: AppSummary | null;
};

export function findAppForSource(source: GitSource | null, apps: AppSummary[]): AppSummary | null {
  const appKey = source?.app_key?.trim();
  if (appKey) {
    const logicalApp = apps.find((app) => app.app_key === appKey);
    if (logicalApp) return logicalApp;
  }
  return source ? apps.find((app) => app.git_source_id === source.id) || null : null;
}

export function buildAppRows(sources: GitSource[], apps: AppSummary[]): AppRow[] {
  const appsByKey = new Map(apps.map((app) => [app.app_key, app]));
  const appsBySourceID = new Map(apps.map((app) => [app.git_source_id, app]));
  const preferredSources = new Map<string, GitSource>();
  for (const source of sources) {
    const appKey = source.app_key?.trim();
    if (!appKey) continue;
    const current = preferredSources.get(appKey);
    if (!current || compareSourceRecency(source, current) > 0) {
      preferredSources.set(appKey, source);
    }
  }

  const rows: AppRow[] = [];
  const attachedApps = new Set<string>();
  for (const source of sources) {
    const appKey = source.app_key?.trim();
    if (appKey) {
      if (preferredSources.get(appKey)?.id !== source.id) continue;
      const app = appsByKey.get(appKey) || appsBySourceID.get(source.id) || null;
      rows.push({ source, app });
      if (app) attachedApps.add(app.app_key);
      continue;
    }

    const app = appsBySourceID.get(source.id) || null;
    if (app && preferredSources.has(app.app_key)) continue;
    rows.push({ source, app });
    if (app) attachedApps.add(app.app_key);
  }

  for (const app of apps) {
    if (!attachedApps.has(app.app_key)) rows.push({ source: null, app });
  }
  return rows;
}

function compareSourceRecency(left: GitSource, right: GitSource): number {
  const leftSynced = timestamp(left.last_synced_at);
  const rightSynced = timestamp(right.last_synced_at);
  if (leftSynced !== rightSynced) return leftSynced - rightSynced;

  const leftCreated = timestamp(left.created_at);
  const rightCreated = timestamp(right.created_at);
  if (leftCreated !== rightCreated) return leftCreated - rightCreated;
  return left.id - right.id;
}

function timestamp(value?: string | null): number {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
}
