export default {
  order: 5,
  id: "release-history",
  title: "Audit release history",
  description:
    "The Releases tab is the audit trail: every record shows who published which commit, from which source, and why.",
  screenshot: "docs/assets/ui/release-history.png",
  guide: [
    "Open an app and switch to the Releases tab.",
    "Each record shows the actor, commit, source, release id, and note.",
    "Use the record to answer who changed the worker-visible contract, and when.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .tableRow");
    await page.clickText("Releases");
    await page.waitForSelector("#releaseHistory .cellTitle");
    await capture(this.id);
  },
};
