export default {
  order: 9,
  id: "settings",
  title: "Connect standalone access",
  description:
    "Access settings connects workspace tokens to the CLI and keeps browser-local credentials isolated to standalone mode.",
  screenshot: "docs/assets/ui/settings.png",
  guide: [
    "Open Settings from the sidebar and choose Access.",
    "Create and store a named workspace token for the CLI or integration.",
    "Copy the control-plane URL and workspace ID into the CLI or integration.",
    "Choose how the CLI stores or receives its credential outside the Web UI.",
    "Configure browser-local token and audit actor values only for a standalone console.",
    "In hosted mode, use the host console instead of Core-local token controls.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/access']");
    await page.waitForSelector(".workspaceTokenCreate");
    await page.waitForText("main", "CLI connection");
    await page.waitForText("main", "Control plane URL");
    await page.waitForText("main", "Workspace ID");
    await page.waitForSelector("#settingsToken");
    await capture(this.id);
  },
};
