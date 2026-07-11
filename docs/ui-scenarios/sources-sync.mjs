export default {
  id: "sources-sync",
  title: "Register and sync a git source",
  description: "Use the Sources view to inspect a registered source and trigger a sync.",
  screenshot: "docs/assets/ui/sources-sync.png",
  guide: [
    "Open the Sources view.",
    "Register a repository with branch, subpath, and credentials reference when needed.",
    "Use Sync to materialize the latest configured commit into the runtime cache.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Sources");
    await page.waitForSelector("#sourceList .table-row");
    await capture(this.id);
  },
};
