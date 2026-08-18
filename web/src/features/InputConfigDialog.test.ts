import { beforeEach, describe, expect, test } from "vitest";
import type { InputConfig } from "../lib/api";
import { inputSettingDefinitions } from "../lib/input-setting-schema";
import { setLocale } from "../shared/i18n";
import { inputConfigPayload, inputConfigRows } from "./InputConfigDialog";

describe("inputConfigPayload", () => {
  beforeEach(() => setLocale("en"));

  test("builds JSON values and locked keys", () => {
    expect(
      inputConfigPayload(
        [
          { key: "region", valueText: '"kr"', locked: false },
          { key: "limits", valueText: '{"daily":10}', locked: true },
        ],
        "orders",
        "client-a",
      ),
    ).toEqual({
      action_key: "orders",
      client_id: "client-a",
      config: { region: "kr", limits: { daily: 10 } },
      locked_keys: ["limits"],
    });
  });

  test("rejects duplicate keys and invalid JSON", () => {
    expect(() =>
      inputConfigPayload(
        [
          { key: "region", valueText: '"kr"', locked: false },
          { key: "region", valueText: '"us"', locked: false },
        ],
        "",
        "",
      ),
    ).toThrow("Duplicate key");
    expect(() =>
      inputConfigPayload([{ key: "region", valueText: "kr", locked: false }], "", ""),
    ).toThrow("valid JSON");
  });

  test("requires secret annotations to use an exact variable reference", () => {
    const definitions = inputSettingDefinitions(
      {},
      { properties: { token: { type: "string", writeOnly: true } } },
    );
    expect(() =>
      inputConfigPayload(
        [{ key: "token", valueText: '"plaintext"', locked: true }],
        "orders",
        "",
        definitions,
      ),
    ).toThrow("$var:");
    expect(
      inputConfigPayload(
        [{ key: "token", valueText: '"$var:secrets/token"', locked: true }],
        "orders",
        "",
        definitions,
      ).config,
    ).toEqual({ token: "$var:secrets/token" });
  });

  test("opens legacy rows with null collections without crashing", () => {
    const existing = {
      workspace_id: "default",
      app_key: "orders",
      action_key: "",
      config: null,
      locked_keys: null,
      updated_by: "operator",
      updated_at: "2026-08-18T00:00:00Z",
    } as unknown as InputConfig;

    expect(inputConfigRows(existing)).toEqual([]);
  });
});
