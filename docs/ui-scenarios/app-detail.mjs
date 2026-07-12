export default {
  order: 3,
  id: "app-detail",
  title: "Inspect an app",
  description:
    "The app detail Overview tab shows the active worker contract, the exposed actions, and readiness signals for the release.",
  screenshot: "docs/assets/ui/app-detail.png",
  guide: [
    "Open an app from the Apps view.",
    "Review the active contract: app key, release commit, entrypoint, route tag, and timeout.",
    "Follow the source code link to browse the repository at the pinned release commit on GitHub/GitLab.",
    "Use the tabs for repository settings, release history, and action schemas.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .tableRow");
    await page.waitForText("h2", "Active contract");
    await capture(this.id);
  },
};
