import { describe, expect, test } from "vitest";
import { parseHostConsoleConfig } from "./runtime-config";

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
