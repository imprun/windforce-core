import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const accessPageSource = await readFile(
  new URL("./WorkspaceAccessSettingsPage.tsx", import.meta.url),
  "utf8",
);

describe("workspace access sections", () => {
  test("keeps the token action aligned with its input in one control row", () => {
    expect(accessPageSource).toContain('className="workspaceTokenCreate"');
    expect(accessPageSource).toContain('className="fieldWithAction"');
    expect(accessPageSource).toContain('htmlFor="workspaceTokenName"');
    expect(accessPageSource).toContain('id="workspaceTokenName"');
    expect(accessPageSource).toContain('type="submit"');
  });

  test("separates hosted access from standalone credential administration", () => {
    expect(accessPageSource).toContain("WorkspaceAccessSections");
    expect(accessPageSource).toContain("<HostedAccessPanel");
    expect(accessPageSource).toContain("<StandaloneAccessSettings");
    expect(accessPageSource).toContain("<CoreAPIConnectionPanel");
    expect(accessPageSource).not.toContain("<LocalBrowserAccessPanel");
    expect(accessPageSource).not.toContain("<Layout");
    expect(accessPageSource).not.toContain("<SettingsNav");
  });

  test("uses the shared confirmation dialog for token rotation and revocation", () => {
    expect(accessPageSource).toContain("<ConfirmDialog");
    expect(accessPageSource).not.toContain("window.confirm");
  });
});
