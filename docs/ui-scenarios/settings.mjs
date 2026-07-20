export default {
  order: 9,
  id: "settings",
  title: "Connect the CLI and set browser access",
  description:
    "General settings shows external CLI connection metadata and stores the API token and audit actor used by Web UI requests.",
  screenshot: "docs/assets/ui/settings.png",
  guide: [
    "Open Settings from the sidebar.",
    "Copy the control plane URL, workspace ID, token environment name, or complete profile command from CLI connection.",
    "Set the named environment variable to the one-time token issued from the workspace Access tab; token values are not included in copied commands.",
    "Set the API token when the control plane requires authentication.",
    "Set the audit actor recorded on releases and cancels; local development defaults to local-dev.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.waitForText("main", "CLI connection");
    await page.waitForText("main", "Control plane URL");
    await page.waitForText("main", "WINDFORCE_CORE_API_TOKEN");
    await page.waitForSelector("#settingsToken");
    await capture(this.id);
  },
};
