import { beforeEach, describe, expect, test } from "vitest";
import { setLocale } from "../shared/i18n";
import {
  formatSystemInfoValue,
  systemInfoLabel,
  visibleRuntimeConfigEntries,
} from "./SettingsInfoPage";

describe("formatSystemInfoValue", () => {
  beforeEach(() => setLocale("en"));

  test("uses neutral language for disabled boolean settings", () => {
    expect(formatSystemInfoValue(true)).toBe("Enabled");
    expect(formatSystemInfoValue(false)).toBe("Not enabled");
  });

  test("keeps empty values compact", () => {
    expect(formatSystemInfoValue("")).toBe("—");
    expect(formatSystemInfoValue(null)).toBe("—");
  });

  test("uses readable labels for known runtime capabilities", () => {
    expect(systemInfoLabel("invocation_api")).toBe("Invocation API");
    expect(systemInfoLabel("http_routes_count")).toBe("HTTP route count");
  });
});

describe("visibleRuntimeConfigEntries", () => {
  test("keeps presentation contracts out of the operator-facing settings list", () => {
    expect(
      visibleRuntimeConfigEntries({
        ui_mode: "embedded",
        worker_group_operator: "external",
        wait_ms: 1500,
        web_ui: true,
      }),
    ).toEqual([
      ["wait_ms", 1500],
      ["web_ui", true],
    ]);
  });
});
