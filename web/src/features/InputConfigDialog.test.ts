import { beforeEach, describe, expect, test } from "vitest";
import { setLocale } from "../shared/i18n";
import { inputConfigPayload } from "./InputConfigDialog";

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
});
