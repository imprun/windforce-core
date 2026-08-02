import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const workspaceSettingsSource = await readFile(
  new URL("./WorkspaceSettingsPage.tsx", import.meta.url),
  "utf8",
);

describe("workspace settings", () => {
  test("uses shared confirmation dialogs for archival and permanent deletion", () => {
    expect(workspaceSettingsSource).toContain("<ConfirmDialog");
    expect(workspaceSettingsSource).toContain("api.deleteWorkspace(workspace.id)");
    expect(workspaceSettingsSource).toContain("expected: workspace.name");
    expect(workspaceSettingsSource).toContain("workspaceSettings.deleteConfirmationLabel");
    expect(workspaceSettingsSource).toContain("danger");
    expect(workspaceSettingsSource).not.toContain("window.confirm");
  });

  test("protects default and switches to it after deleting another workspace", () => {
    expect(workspaceSettingsSource).toContain('runtimeConfig?.authMode === "host_managed"');
    expect(workspaceSettingsSource).toContain('workspace.id === "default"');
    expect(workspaceSettingsSource).toContain('workspace: "default"');
    expect(workspaceSettingsSource).toContain('navigate("/")');
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
