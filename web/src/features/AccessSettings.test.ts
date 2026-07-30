import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const accessSettingsSource = await readFile(
  new URL("./AccessSettings.tsx", import.meta.url),
  "utf8",
);

describe("access settings", () => {
  test("leaves credential storage to the CLI instead of prescribing an environment variable", () => {
    expect(accessSettingsSource).not.toContain("WINDFORCE_CORE_API_TOKEN");
    expect(accessSettingsSource).not.toContain("--token-env");
    expect(accessSettingsSource).not.toContain("cliProfileCommand");
  });

  test("reports successful copies in the button instead of a toast", () => {
    expect(accessSettingsSource).toContain("setCopied(true)");
    expect(accessSettingsSource).toContain('translate("common.copied")');
    expect(accessSettingsSource).not.toContain('notify("ok", translate("common.copiedNamed"');
  });
});
