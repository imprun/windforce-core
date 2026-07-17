export default {
  order: 3.5,
  id: "sync-source",
  title: "Synchronize source",
  description:
    "Sync source fetches the tracked branch, validates the source contract, and stores the exact revision without changing the active release.",
  screenshot: "docs/assets/ui/sync-source.png",
  guide: [
    "Open an app and switch to the Repository tab.",
    "Click Sync source.",
    "Confirm that Latest synchronized source shows the fetched commit.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .tableRow");
    await page.clickText("Repository");
    await page.clickText("Sync source");
    await page.waitForSelector(".inlineNotice.success");
    await capture(this.id);
  },
};
