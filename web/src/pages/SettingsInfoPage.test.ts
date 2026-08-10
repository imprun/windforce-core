import { readFile } from "node:fs/promises";
import { beforeEach, describe, expect, test } from "vitest";
import { setLocale } from "../shared/i18n";
import { formatSystemInfoValue, systemInfoLabel } from "./SettingsInfoPage";

const source = await readFile(new URL("./SettingsInfoPage.tsx", import.meta.url), "utf8");

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

describe("Worker Group operations guidance", () => {
  test("routes externally operated groups to the configured neutral host console", () => {
    expect(source).toContain('runtimeConfig?.workerGroupOperator === "external"');
    expect(source).toContain("runtimeConfig.hostConsole.url");
    expect(source).toContain("runtimeConfig.hostConsole.label");
    expect(source).toContain('translate("info.workerGroupOperator.metadataOnly")');
  });
});
