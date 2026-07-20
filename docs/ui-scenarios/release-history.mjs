export default {
  order: 5,
  id: "release-history",
  title: "Review release history",
  description:
    "The Releases tab is the publish history of the worker-visible contract: who published which commit, from which source, and why. Configuration changes live on the Audit tab.",
  screenshot: "docs/assets/ui/release-history.png",
  guide: [
    "Open an app and switch to the Releases tab.",
    "Each release record shows the actor, commit, source, release id, and note.",
    "Use it to answer who published which contract, and when; configuration changes are on the Audit tab.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.clickText("Releases");
    await page.waitForSelector("#releaseHistory .cellTitle");
    await capture(this.id);
  },
};
