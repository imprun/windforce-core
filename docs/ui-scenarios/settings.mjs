export default {
  order: 9,
  id: "settings",
  title: "Set API access and audit context",
  description:
    "General settings holds the API token and local audit actor used by Web UI requests. Values are stored in the browser.",
  screenshot: "docs/assets/ui/settings.png",
  guide: [
    "Open Settings from the sidebar.",
    "Set the API token when the control plane requires authentication.",
    "Set the audit actor recorded on releases and cancels; local development defaults to local-dev.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.waitForSelector("#settingsToken");
    await capture(this.id);
  },
};
