import { describe, expect, test } from "bun:test";
import type { Workspace } from "./api";
import { visibleWorkspaces } from "./workspaces";

const base: Omit<Workspace, "id" | "name" | "status"> = {
  has_token: false,
  created_by: "system",
  created_at: "2026-01-01T00:00:00Z",
  updated_by: "system",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("visibleWorkspaces", () => {
  test("shows active workspaces and keeps the current archived workspace visible", () => {
    const workspaces: Workspace[] = [
      { ...base, id: "default", name: "Default", status: "active" },
      { ...base, id: "archived-current", name: "Archived current", status: "archived" },
      { ...base, id: "archived-other", name: "Archived other", status: "archived" },
    ];

    expect(visibleWorkspaces(workspaces, "archived-current").map((workspace) => workspace.id)).toEqual([
      "default",
      "archived-current",
    ]);
  });
});
