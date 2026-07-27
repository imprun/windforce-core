import { describe, expect, test } from "vitest";
import { workspaceDetailTarget } from "./WorkspaceDetailPage";

describe("workspaceDetailTarget", () => {
  test("keeps old workspace detail links compatible with the settings information architecture", () => {
    expect(workspaceDetailTarget("overview")).toBe("/settings/workspace");
    expect(workspaceDetailTarget("lifecycle")).toBe("/settings/workspace");
    expect(workspaceDetailTarget("access")).toBe("/settings/access");
    expect(workspaceDetailTarget("audit")).toBe("/audit");
  });
});
