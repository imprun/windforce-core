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
    ).toEqual({ hostConsole: null, hostAccount: null });
    expect(parseRuntimeConfig({ host_account: { endpoint: "//portal.example.test/me" } })).toEqual({
      hostConsole: null,
      hostAccount: null,
    });
  });
});
