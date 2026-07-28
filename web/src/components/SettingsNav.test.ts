import { describe, expect, test } from "vitest";
import { settingsNavItems } from "./SettingsNav";

describe("settingsNavItems", () => {
  test("keeps operational settings before read-only information", () => {
    expect(settingsNavItems.map((item) => item.labelKey)).toEqual([
      "settingsNav.general",
      "settingsNav.workspace",
      "settingsNav.access",
      "settingsNav.provisioning",
      "settingsNav.webhooks",
      "settingsNav.info",
    ]);
  });
});
