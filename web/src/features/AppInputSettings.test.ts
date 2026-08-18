import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, test } from "vitest";
import type { InputConfig } from "../lib/api";
import { formatInputSettingValue, InputSettingScopeList } from "./InputSettingScopeList";

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

describe("InputSettingScopeList", () => {
  test("renders legacy null collections without crashing", () => {
    const config = {
      workspace_id: "default",
      app_key: "orders",
      action_key: "",
      config: { region: "kr" },
      locked_keys: null,
      updated_by: "operator",
      updated_at: "2026-08-18T00:00:00Z",
    } as unknown as InputConfig;
    const html = renderToStaticMarkup(
      createElement(InputSettingScopeList, {
        id: "settings",
        items: [
          {
            key: "default",
            config,
            primaryLabel: "Scope",
            primaryValue: "Default",
            primaryMeta: "All callers",
            actionName: "All actions",
            actionMeta: "*",
            editLabel: "Edit",
            onEdit: () => undefined,
          },
        ],
      }),
    );

    expect(html).toContain("region");
    expect(html).toContain("kr");
  });
});
