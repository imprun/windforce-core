export default {
  order: 9.5,
  id: "workspace-switcher-mobile",
  title: "Switch workspace on a narrow screen",
  description:
    "The mobile drawer keeps workspace context separate from the compact top bar and opens the same switcher and administration menu.",
  screenshot: "docs/assets/ui/workspace-switcher-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open the mobile navigation drawer.",
    "Open the workspace control below the Windforce runtime identity.",
    "Choose another workspace or open Manage workspaces without returning to desktop navigation.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click('button[aria-label="Open navigation menu"]');
    await page.waitForSelector(".platformMobileNav");
    await page.click(".platformMobileNav .workspaceSwitcherTrigger");
    await page.waitForSelector(".platformMobileNav .workspacePopover");
    await page.waitForText(".platformMobileNav .workspacePopover", "Operations");
    await capture(this.id);
  },
};
