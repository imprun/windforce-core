import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const workspaceSettingsSource = await readFile(
  new URL("./WorkspaceSettingsPage.tsx", import.meta.url),
  "utf8",
);

describe("workspace settings", () => {
  test("uses the shared confirmation dialog for archival", () => {
    expect(workspaceSettingsSource).toContain("<ConfirmDialog");
    expect(workspaceSettingsSource).not.toContain("window.confirm");
  });

  test("keeps the display-name action on the input control row", () => {
    expect(workspaceSettingsSource).toContain('<div className="fieldWithAction">');
  });

  test("combines identity, API access, connection details, and lifecycle", () => {
    expect(workspaceSettingsSource).toContain("<WorkspaceAccessSections");
    expect(workspaceSettingsSource).toContain('translate("workspaceSettings.identity")');
    expect(workspaceSettingsSource).toContain('translate("workspaceSettings.lifecycle")');
  });
});
