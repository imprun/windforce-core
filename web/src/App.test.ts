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
