export default {
  order: 9.5,
  id: "workspace-switcher-mobile",
  title: "Switch workspace on a narrow screen",
  description:
    "The compact top-bar breadcrumb keeps workspace context visible and opens the same switcher and administration menu.",
  screenshot: "docs/assets/ui/workspace-switcher-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open the workspace control in the top-bar breadcrumb.",
    "Choose another workspace or open Manage workspaces without opening the navigation drawer.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click("[data-testid='workspace-topbar-context'] .workspaceSwitcherTrigger");
    await page.waitForSelector("[data-testid='workspace-topbar-context'] .workspacePopover");
    await page.waitForText("[data-testid='workspace-topbar-context'] .workspacePopover", "Operations");
    await capture(this.id);
  },
};
