import { type AppSettingsTabKey, appSettingsPath } from "../lib/app-settings-navigation";
import { Link } from "../lib/router";
import { type TranslationKey, translate } from "../shared/i18n";

export const appSettingsNavItems = [
  { key: "repository", labelKey: "audit.repository" as TranslationKey },
  { key: "input-settings", labelKey: "audit.inputSettings" as TranslationKey },
  { key: "runtime-config", labelKey: "appDetail.tab.runtimeConfig" as TranslationKey },
  { key: "placement", labelKey: "appDetail.tab.placement" as TranslationKey },
  { key: "execution-limits", labelKey: "appDetail.tab.executionLimits" as TranslationKey },
] as const satisfies ReadonlyArray<{ key: AppSettingsTabKey; labelKey: TranslationKey }>;

export function AppSettingsNav({
  sourceID,
  activeTab,
  repositoryAvailable,
}: {
  sourceID: number;
  activeTab: AppSettingsTabKey;
  repositoryAvailable: boolean;
}) {
  const items = repositoryAvailable
    ? appSettingsNavItems
    : appSettingsNavItems.filter((item) => item.key !== "repository");

  return (
    <nav
      className="tabBar settingsNav appSettingsNav"
      aria-label={translate("appDetail.settingsTabs")}
      data-ui-guide="app-settings-nav"
    >
      {items.map((item) => (
        <Link
          key={item.key}
          className={item.key === activeTab ? "tab active" : "tab"}
          to={appSettingsPath(sourceID, item.key)}
        >
          {translate(item.labelKey)}
        </Link>
      ))}
    </nav>
  );
}
