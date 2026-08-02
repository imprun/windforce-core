import { Link, useRouter } from "../lib/router";
import { type TranslationKey, translate } from "../shared/i18n";

export const settingsNavItems = [
  {
    to: "/settings/workspace",
    labelKey: "settingsNav.workspace" as TranslationKey,
    match: (path: string) => path === "/settings" || path === "/settings/workspace",
  },
  {
    to: "/settings/webhooks",
    labelKey: "settingsNav.webhooks" as TranslationKey,
    match: (path: string) => path.startsWith("/settings/webhooks"),
  },
  {
    to: "/settings/variables",
    labelKey: "settingsNav.variablesResources" as TranslationKey,
    match: (path: string) => path === "/settings/variables",
  },
  {
    to: "/settings/provisioning",
    labelKey: "settingsNav.provisioning" as TranslationKey,
    match: (path: string) => path === "/settings/provisioning",
  },
  {
    to: "/settings/system",
    labelKey: "settingsNav.system" as TranslationKey,
    match: (path: string) => path === "/settings/system" || path === "/settings/info",
  },
];

export function SettingsNav() {
  const { path } = useRouter();
  return (
    <nav className="tabBar settingsNav" aria-label={translate("settingsNav.ariaLabel")}>
      {settingsNavItems.map((item) => (
        <Link key={item.to} className={item.match(path) ? "tab active" : "tab"} to={item.to}>
          {translate(item.labelKey)}
        </Link>
      ))}
    </nav>
  );
}
