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

  test("uses the topbar breadcrumb for workspace context and keeps account context in the sidebar", () => {
    expect(layoutSource).toContain('data-testid="workspace-topbar-context"');
    expect(layoutSource).not.toContain("sidebarWorkspaceContext");
    expect(layoutSource).toContain("sidebarFooter grid");
    expect(layoutSource).toContain("AccountContext");
    expect(layoutSource).toContain('<WorkspaceSwitcher variant="breadcrumb"');
  });

  test("keeps hosted-console navigation workspace-scoped and vendor neutral", () => {
    expect(layoutSource).not.toContain('placement="sidebar" collapsed={collapsed}');
    expect(layoutSource.match(/<HostConsoleAction hostConsole=/g)).toHaveLength(1);
    expect(layoutSource).toContain("{hostConsole.label}");
  });

  test("distinguishes hosted accounts from standalone local access", () => {
    expect(layoutSource).toContain("HostedAccountMenu");
    expect(layoutSource).toContain("Local access");
    expect(layoutSource).not.toContain(">Browser access<");
  });
});
