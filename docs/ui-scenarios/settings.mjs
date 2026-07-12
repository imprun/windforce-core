export default {
  order: 9,
  id: "settings",
  title: "Set the control-plane context",
  description:
    "Settings holds the workspace, API token, and audit actor that every Web UI request uses. Values are stored in the browser.",
  screenshot: "docs/assets/ui/settings.png",
  guide: [
    "Open Settings from the sidebar.",
    "Set the workspace and, when the control plane requires one, the API token.",
    "Set the audit actor recorded on releases and cancels; local development defaults to local-dev.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.waitForSelector("#settingsWorkspace");
    await capture(this.id);
  },
};
