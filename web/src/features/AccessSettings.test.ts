import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const accessSettingsSource = await readFile(
  new URL("./AccessSettings.tsx", import.meta.url),
  "utf8",
);

describe("access settings", () => {
  test("keeps Core access independent from the Cloud CLI", () => {
    expect(accessSettingsSource).toContain("CoreAPIConnectionPanel");
    expect(accessSettingsSource).toContain("workspaceAPIURL");
    expect(accessSettingsSource).toContain('to="/clients"');
    expect(accessSettingsSource).not.toContain("CLIConnectionPanel");
    expect(accessSettingsSource).not.toContain("WINDFORCE_CORE_API_TOKEN");
    expect(accessSettingsSource).not.toContain("--token-env");
    expect(accessSettingsSource).not.toContain("cliProfileCommand");
  });

  test("delegates hosted credential management without presenting a Core CLI guide", () => {
    expect(accessSettingsSource).toContain("HostedAccessPanel");
    expect(accessSettingsSource).toContain('translate("settings.manageAccessInHost")');
    expect(accessSettingsSource).not.toContain("HostedAccessPanels");
    expect(accessSettingsSource).not.toContain("settings.hostedCLI");
  });

  test("reports successful copies in the button instead of a toast", () => {
    expect(accessSettingsSource).toContain("setCopied(true)");
    expect(accessSettingsSource).toContain('translate("common.copied")');
    expect(accessSettingsSource).not.toContain('notify("ok", translate("common.copiedNamed"');
  });

  test("keeps browser credentials in a focused dialog instead of an access-page card", () => {
    expect(accessSettingsSource).toContain("LocalBrowserAccessDialog");
    expect(accessSettingsSource).toContain("<Modal");
    expect(accessSettingsSource).not.toContain("LocalBrowserAccessPanel");
    expect(accessSettingsSource).not.toContain('className="localAccessFooter"');
  });
});
