import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";
import { formatInputSettingValue } from "./InputSettingScopeList";

const source = await readFile(new URL("./AppInputSettings.tsx", import.meta.url), "utf8");

describe("formatInputSettingValue", () => {
  test("preserves JSON scalar types", () => {
    expect(formatInputSettingValue("30")).toBe('"30"');
    expect(formatInputSettingValue(30)).toBe("30");
    expect(formatInputSettingValue(false)).toBe("false");
    expect(formatInputSettingValue(null)).toBe("null");
  });

  test("formats structured values for direct inspection", () => {
    expect(formatInputSettingValue({ region: "kr", retries: 2 })).toBe(`{
  "region": "kr",
  "retries": 2
}`);
  });
});

describe("app-caller input-setting navigation", () => {
  test("keeps editing in app detail and links caller identity to its access overview", () => {
    expect(source).toMatch(/<Link to=\{`\/clients\/\$\{selectedClient\.id\}`\}>/);
    expect(source).not.toMatch(/\/clients\/\$\{selectedClient\.id\}\/input-settings/);
  });
});
