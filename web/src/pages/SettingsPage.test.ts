import { describe, expect, test } from "vitest";
import { CLI_TOKEN_ENV, cliProfileCommand } from "./SettingsPage";

describe("CLI connection settings", () => {
  test("builds a token-free profile command for the active workspace", () => {
    expect(cliProfileCommand("https://windforce.example.test", "gale")).toBe(
      `windforce profile set gale --api-url "https://windforce.example.test" --workspace gale --token-env ${CLI_TOKEN_ENV} --use`,
    );
  });

  test("uses the supported default token environment", () => {
    expect(CLI_TOKEN_ENV).toBe("WINDFORCE_CORE_API_TOKEN");
  });
});
