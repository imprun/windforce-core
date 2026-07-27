export default {
  order: 9,
  id: "workspace-switcher",
  title: "Switch workspace context",
  description:
    "The top-bar breadcrumb keeps runtime scope visible and provides the entry point to instance workspace administration.",
  screenshot: "docs/assets/ui/workspace-switcher.png",
  guide: [
    "Open the workspace control in the top-bar breadcrumb.",
    "Select a workspace to change the active application and monitoring scope.",
    "Choose Manage workspaces to create or switch workspaces; configure the active workspace from Settings.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click("[data-testid='workspace-topbar-context'] .workspaceSwitcherTrigger");
    await page.waitForSelector("[data-testid='workspace-topbar-context'] .workspacePopover");
    await page.waitForText("[data-testid='workspace-topbar-context'] .workspacePopover", "Operations");
    await capture(this.id);
  },
};
