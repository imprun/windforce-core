import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const registrySource = await readFile(new URL("./WorkspacesPage.tsx", import.meta.url), "utf8");
const settingsSource = await readFile(
  new URL("./WorkspaceSettingsPage.tsx", import.meta.url),
  "utf8",
);
const adminSource = await readFile(
  new URL("../features/WorkspaceAdmin.tsx", import.meta.url),
  "utf8",
);

describe("workspace administration shell", () => {
  test("uses the instance shell for the registry", () => {
    expect(registrySource).toContain('scope="instance"');
  });

  test("offers workspace switching only from the registry", () => {
    expect(registrySource).toContain("<WorkspaceActivation workspace={workspace} compact />");
    expect(adminSource).toContain("updateSettings({ ...settings, workspace: workspace.id })");
    expect(adminSource).toContain('navigate("/")');
    expect(registrySource).not.toContain("Manage");
  });

  test("uses a constrained identity grid", () => {
    expect(settingsSource).toContain('className="workspaceIdentityFacts"');
  });
});
