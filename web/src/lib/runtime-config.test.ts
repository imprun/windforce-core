import { describe, expect, test } from "vitest";
import { parseHostConsoleConfig, parseRuntimeConfig } from "./runtime-config";

describe("parseHostConsoleConfig", () => {
  test("accepts a configured HTTP host console", () => {
    expect(
      parseHostConsoleConfig({
        host_console: {
          url: "https://portal.example.test/console",
          label: "Back to operations portal",
        },
      }),
    ).toEqual({
      url: "https://portal.example.test/console",
      label: "Back to operations portal",
    });
  });

  test("rejects absent, invalid, and executable targets", () => {
    expect(parseHostConsoleConfig({})).toBeNull();
    expect(
      parseHostConsoleConfig({
        host_console: { url: "javascript:alert(1)", label: "Back" },
      }),
    ).toBeNull();
    expect(
      parseHostConsoleConfig({
        host_console: { url: "https://portal.example.test", label: "   " },
      }),
    ).toBeNull();
  });
});

describe("parseRuntimeConfig", () => {
  test("accepts a same-origin hosted account endpoint", () => {
    expect(
      parseRuntimeConfig({
        host_account: { endpoint: "/_host/account" },
      }).hostAccount,
    ).toEqual({ endpoint: "/_host/account" });
  });

  test("rejects cross-origin and protocol-relative hosted account endpoints", () => {
    expect(
      parseRuntimeConfig({ host_account: { endpoint: "https://portal.example.test/me" } }),
    ).toEqual({
      hostConsole: null,
      hostAccount: null,
      authMode: "disabled",
      uiMode: "embedded",
      workerGroupOperator: "self-managed",
    });
    expect(parseRuntimeConfig({ host_account: { endpoint: "//portal.example.test/me" } })).toEqual({
      hostConsole: null,
      hostAccount: null,
      authMode: "disabled",
      uiMode: "embedded",
      workerGroupOperator: "self-managed",
    });
  });

  test("uses browser-local credentials only when Core explicitly requires them", () => {
    expect(parseRuntimeConfig({ auth_mode: "browser_token" }).authMode).toBe("browser_token");
    expect(parseRuntimeConfig({}).authMode).toBe("disabled");
    expect(parseRuntimeConfig({ auth_mode: "host_managed" }).authMode).toBe("disabled");
  });

  test("lets a validated host account own authentication", () => {
    expect(
      parseRuntimeConfig({
        auth_mode: "browser_token",
        host_account: { endpoint: "/_host/account" },
      }).authMode,
    ).toBe("host_managed");
  });

  test("keeps UI presentation and Worker Group ownership independent", () => {
    expect(
      parseRuntimeConfig({
        ui_mode: "disabled",
        worker_group_operator: "external",
      }),
    ).toMatchObject({ uiMode: "disabled", workerGroupOperator: "external" });
    expect(
      parseRuntimeConfig({
        ui_mode: "hosted",
        worker_group_operator: "core",
      }),
    ).toMatchObject({ uiMode: "embedded", workerGroupOperator: "self-managed" });
  });
});
