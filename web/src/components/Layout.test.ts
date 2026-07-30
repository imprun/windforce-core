import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";
import { primaryNavItems } from "./Layout";

const layoutSource = await readFile(new URL("./Layout.tsx", import.meta.url), "utf8");

describe("primaryNavItems", () => {
  test("keeps workspace administration out of workspace-scoped navigation", () => {
    expect(primaryNavItems.map((item) => item.labelKey)).toEqual([
      "navigation.apps",
      "navigation.clientRegistry",
      "navigation.monitoring",
      "navigation.audit",
      "navigation.settings",
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
    expect(layoutSource).toContain('translate("shell.localAccess")');
    expect(layoutSource).not.toContain(">Browser access<");
  });

  test("uses a globe and names the locale that the switcher will activate", () => {
    expect(layoutSource).toContain("<Globe2");
    expect(layoutSource).toContain('locale === "ko" ? "en" : "ko"');
    expect(layoutSource).toContain('nextLocale === "ko" ? translate("language.korean") : "EN"');
    expect(layoutSource).not.toContain("가/A");
  });
});
