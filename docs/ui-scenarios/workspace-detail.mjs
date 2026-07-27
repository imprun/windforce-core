export default {
  order: 11,
  id: "workspace-detail",
  title: "Configure the active workspace",
  description:
    "Identity and lifecycle settings stay with the active workspace instead of creating a second registry detail context.",
  screenshot: "docs/assets/ui/workspace-detail.png",
  guide: [
    "Switch to the workspace from Manage workspaces.",
    "Open Settings and choose Workspace to change its display name or archive it.",
    "Choose Access to issue, rotate, or revoke named workspace credentials.",
    "Open Audit and select Workspace to review identity, credential, and lifecycle events.",
    "Return to Manage workspaces only when creating or switching workspace context.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/workspace']");
    await page.waitForText("main", "Workspace identity");
    await page.waitForText("main", "Workspace lifecycle");
    await capture(this.id);
  },
};
