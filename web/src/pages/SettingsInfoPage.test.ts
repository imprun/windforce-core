import { beforeEach, describe, expect, test } from "vitest";
import { setLocale } from "../shared/i18n";
import { formatSystemInfoValue } from "./SettingsInfoPage";

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
});
