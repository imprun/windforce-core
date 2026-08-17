export const appSettingsTabKeys = [
  "repository",
  "input-settings",
  "runtime-config",
  "placement",
  "execution-limits",
] as const;

export type AppSettingsTabKey = (typeof appSettingsTabKeys)[number];

export function isAppSettingsTabKey(value: string | undefined): value is AppSettingsTabKey {
  return appSettingsTabKeys.some((key) => key === value);
}

export function defaultAppSettingsTab(repositoryAvailable: boolean): AppSettingsTabKey {
  return repositoryAvailable ? "repository" : "input-settings";
}

export function appSettingsPath(
  sourceID: number | string,
  tab: AppSettingsTabKey,
  section?: string,
  actionKey?: string,
): string {
  const segments = [
    "apps",
    String(sourceID),
    "settings",
    tab,
    ...(section ? [section] : []),
    ...(actionKey ? [actionKey] : []),
  ];
  return `/${segments.map(encodeURIComponent).join("/")}`;
}
