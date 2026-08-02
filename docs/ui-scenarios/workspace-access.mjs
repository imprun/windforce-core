export default {
  order: 12,
  id: "workspace-access",
  title: "Manage workspace access",
  description:
    "Named credentials belong to the active workspace and keep issue, rotation, and revocation separate from workspace creation.",
  screenshot: "docs/assets/ui/workspace-access.png",
  guide: [
    "Switch to the workspace that will own the credential.",
    "Open Settings and choose Workspace.",
    "Name each credential for its operator, administrative tool, or recovery purpose.",
    "Store the raw secret when it is shown; later views expose metadata only.",
    "Rotate or revoke one credential without interrupting the workspace's other callers.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("a[href$='/settings']");
    await page.click("a[href$='/settings']");
    await page.click("a[href$='/settings/workspace']");
    await page.waitForSelector(".workspaceTokenCreate");
    await capture(this.id);
  },
};
