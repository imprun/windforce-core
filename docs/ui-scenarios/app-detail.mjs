export default {
  order: 3,
  id: "app-detail",
  title: "Inspect an app",
  description:
    "The app detail Overview keeps immutable release defaults separate from effective execution placement and worker readiness.",
  screenshot: "docs/assets/ui/app-detail.png",
  guide: [
    "Open an app from the Apps view.",
    "Review the active release: app key, release commit, entrypoint, and update time.",
    "Follow the source code link to browse the repository at the pinned release commit on GitHub/GitLab.",
    "Follow the effective worker tag to App settings > Placement when you need to inspect or edit worker selection.",
    "Use App settings for repository and execution configuration; keep release history and action schemas in the top-level tabs.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.waitForSelector(".readinessFacts");
    await capture(this.id);
  },
};
