export default {
  order: 11,
  id: "workspace-detail",
  title: "Administer a workspace",
  description:
    "Each workspace has a dedicated instance-administration page without workspace-scoped application navigation.",
  screenshot: "docs/assets/ui/workspace-detail.png",
  guide: [
    "Open Manage workspaces from the workspace switcher, then choose a workspace from the registry.",
    "Use Workspaces in the instance breadcrumb to return to the registry.",
    "Use Switch to workspace to select this workspace and open its application console.",
    "Use Overview for its display name, Access for its scoped token, Audit for lifecycle history, and Lifecycle for archive controls.",
    "Return to the runtime and use its breadcrumb when changing workspace scope.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click("[data-testid='workspace-topbar-context'] .workspaceSwitcherTrigger");
    await page.click("[data-testid='workspace-topbar-context'] .workspaceManageLink");
    await page.waitForSelector("#workspaceRegistry tbody tr");
    await page.clickText("Operations");
    await page.waitForText("main", "Workspace identity");
    await page.waitForText(".topbarActions", "Switch to workspace");
    await capture(this.id);
  },
};
