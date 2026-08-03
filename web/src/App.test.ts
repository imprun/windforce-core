import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const source = await readFile(new URL("./App.tsx", import.meta.url), "utf8");

describe("settings routes", () => {
  test("uses canonical workspace and variables routes with legacy redirects", () => {
    expect(source).toContain('<RouteRedirect to="/settings/workspace"');
    expect(source).toContain('<RouteRedirect to="/settings/variables"');
    expect(source).toContain('matchRoute("/settings/variables", path)');
    expect(source).toContain("<VariablesResourcesPage");
    expect(source).not.toContain("WorkspaceAccessSettingsPage />");
  });
});

describe("HumanTask routes", () => {
  test("keeps the generic HumanTask queue in the workspace shell", () => {
    expect(source).toContain('matchRoute("/human-tasks", path)');
    expect(source).toContain("<HumanTasksPage");
  });
});
