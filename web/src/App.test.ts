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

describe("app-caller routes", () => {
  test("redirects removed app-caller input-setting routes to the caller overview", () => {
    expect(source).toContain('matchRoute("/clients/:id/input-settings/:appKey?", path)');
    expect(source).toMatch(
      /<RouteRedirect to=\{`\/clients\/\$\{encodeURIComponent\(legacyClientInputSettings\.id\)\}`\} \/>/,
    );
    expect(source).toContain('matchRoute("/clients/:id/:tab?", path)');
    expect(source).not.toContain('matchRoute("/clients/:id/:tab?/:appKey?", path)');
  });
});

describe("HumanTask routes", () => {
  test("keeps the generic HumanTask queue in the workspace shell", () => {
    expect(source).toContain('matchRoute("/human-tasks", path)');
    expect(source).toContain("<HumanTasksPage");
  });
});

describe("WorkerGroup routes", () => {
  test("uses the user-facing execution-pools route and redirects the technical legacy route", () => {
    expect(source).toContain('matchRoute("/execution-pools", path)');
    expect(source).toContain(
      'matchRoute("/worker-groups", path)) return <RouteRedirect to="/execution-pools"',
    );
    expect(source).toContain("<WorkerGroupsPage");
  });
});
