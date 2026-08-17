import { describe, expect, test } from "vitest";
import {
  appSettingsPath,
  appSettingsTabKeys,
  defaultAppSettingsTab,
  isAppSettingsTabKey,
} from "./app-settings-navigation";

describe("App settings navigation", () => {
  test("keeps the approved settings order", () => {
    expect(appSettingsTabKeys).toEqual([
      "repository",
      "input-settings",
      "runtime-config",
      "placement",
      "execution-limits",
    ]);
  });

  test("uses repository when available and input settings for source-removed apps", () => {
    expect(defaultAppSettingsTab(true)).toBe("repository");
    expect(defaultAppSettingsTab(false)).toBe("input-settings");
  });

  test("builds canonical settings routes without losing a selected Client", () => {
    expect(appSettingsPath(7, "placement")).toBe("/apps/7/settings/placement");
    expect(appSettingsPath(7, "input-settings", "client", "client A")).toBe(
      "/apps/7/settings/input-settings/client/client%20A",
    );
  });

  test("recognizes only App settings tabs", () => {
    expect(isAppSettingsTabKey("runtime-config")).toBe(true);
    expect(isAppSettingsTabKey("monitoring")).toBe(false);
  });
});
