import { describe, expect, test } from "vitest";
import { appSettingsNavItems } from "./AppSettingsNav";

describe("appSettingsNavItems", () => {
  test("uses product-neutral existing labels for every approved settings surface", () => {
    expect(appSettingsNavItems.map((item) => item.labelKey)).toEqual([
      "audit.repository",
      "audit.inputSettings",
      "appDetail.tab.runtimeConfig",
      "appDetail.tab.placement",
      "appDetail.tab.executionLimits",
    ]);
  });
});
