export default {
  order: 4,
  id: "publish-release",
  title: "Publish a release",
  description:
    "Publish Release validates the repository source at HEAD and publishes it as the worker-visible contract, recorded with the audit actor.",
  screenshot: "docs/assets/ui/publish-release.png",
  guide: [
    "Open an app and click Publish Release.",
    "Confirm the repository, branch, subpath, and current release commit.",
    "Add a release note for the audit trail.",
    "Publish; the release history records the actor, commit, and note.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.actor", "ui-guide@example.test"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .tableRow");
    await page.waitForSelector("#publishReleaseButton");
    await page.click("#publishReleaseButton");
    await page.waitForSelector("#publishReleaseDialog");
    await page.fill("#publishReleaseMessage", "Ship the new echo contract");
    await capture(this.id);
  },
};
