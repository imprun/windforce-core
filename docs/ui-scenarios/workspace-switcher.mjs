export default {
  order: 9,
  id: "workspace-switcher",
  title: "Switch workspace context",
  description:
    "The sidebar keeps the current workspace visible above product navigation and provides the entry point to instance workspace administration.",
  screenshot: "docs/assets/ui/workspace-switcher.png",
  guide: [
    "Open the workspace control at the top of the sidebar.",
    "Select a workspace to change the active application and monitoring scope.",
    "Choose Manage workspaces to create workspaces or manage identity, access, audit, and lifecycle settings.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click(".sidebarWorkspaceContext .workspaceSwitcherTrigger");
    await page.waitForSelector(".sidebarWorkspaceContext .workspacePopover");
    await page.waitForText(".sidebarWorkspaceContext .workspacePopover", "Operations");
    await capture(this.id);
  },
};
