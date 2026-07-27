export default {
  order: 10,
  id: "workspaces",
  title: "Manage workspaces",
  description:
    "The workspace registry is limited to creating and switching workspace context.",
  screenshot: "docs/assets/ui/workspaces.png",
  guide: [
    "Open the workspace switcher in the top-bar breadcrumb.",
    "Choose Manage workspaces to review the instance registry.",
    "Use Switch to select a workspace for application and monitoring operations; the selected row is marked Current.",
    "Use the Windforce breadcrumb root to return to the active workspace console.",
    "Create a workspace, then switch to it before configuring its settings.",
    "Use an instance-admin token for registry operations; workspace tokens remain scoped to one workspace.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click("[data-testid='workspace-topbar-context'] .workspaceSwitcherTrigger");
    await page.click("[data-testid='workspace-topbar-context'] .workspaceManageLink");
    await page.waitForSelector("#workspaceRegistry tbody tr");
    await page.waitForText("#workspaceRegistry", "Operations");
    await page.waitForText("#workspaceRegistry", "Current");
    await page.waitForText("#workspaceRegistry", "Switch");
    await page.waitForText("#workspaceRegistry", "Status");
    await capture(this.id);
  },
};
