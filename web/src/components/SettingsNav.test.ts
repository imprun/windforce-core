import { describe, expect, test } from "vitest";
import { settingsNavItems } from "./SettingsNav";

describe("settingsNavItems", () => {
  test("organizes settings by workspace responsibility and keeps system diagnostics last", () => {
    expect(settingsNavItems.map((item) => item.labelKey)).toEqual([
      "settingsNav.workspace",
      "settingsNav.webhooks",
      "settingsNav.variablesResources",
      "settingsNav.provisioning",
      "settingsNav.system",
    ]);
  });

  test("treats the settings root and legacy info route as compatible aliases", () => {
    expect(settingsNavItems[0]?.match("/settings")).toBe(true);
    expect(settingsNavItems.at(-1)?.match("/settings/info")).toBe(true);
  });

  test("keeps access inside workspace settings and uses the variables route", () => {
    expect(settingsNavItems.map((item) => item.to)).not.toContain("/settings/access");
    expect(settingsNavItems.map((item) => item.to)).toContain("/settings/variables");
  });
});
