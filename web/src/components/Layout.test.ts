import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";
import { primaryNavItems } from "./Layout";

const layoutSource = await readFile(new URL("./Layout.tsx", import.meta.url), "utf8");

describe("primaryNavItems", () => {
  test("keeps workspace administration out of workspace-scoped navigation", () => {
    expect(primaryNavItems.map((item) => item.label)).toEqual([
      "Apps",
      "Client Registry",
      "Monitoring",
      "Audit",
      "Settings",
    ]);
  });

  test("keeps workspace and account context in the balanced sidebar", () => {
    expect(layoutSource).not.toContain('data-testid="workspace-topbar-context"');
    expect(layoutSource).toContain("sidebarWorkspaceContext");
    expect(layoutSource).toContain("sidebarFooter grid");
    expect(layoutSource).toContain('<UserMenu placement="sidebar"');
    expect(layoutSource).toContain('<HostConsoleAction placement="sidebar"');
    expect(layoutSource.match(/<WorkspaceSwitcher \/>/g)?.length).toBe(2);
  });

  test("keeps hosted-console navigation vendor neutral", () => {
    expect(layoutSource.match(/\{hostConsole\.label\}/g)).toHaveLength(4);
  });
});
