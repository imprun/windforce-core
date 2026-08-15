export default {
  order: 3.05,
  id: "placement-candidates",
  title: "Review App and Action execution candidates",
  description:
    "App placement keeps current eligible capacity separate from queued Runs whose execution targets were pinned at admission.",
  screenshot: "docs/assets/ui/placement-candidates.png",
  guide: [
    "Open an App and choose Placement.",
    "Review the App default capacity and its queued demand before the Action-specific rows.",
    "Current placement shows occupied and free compatible slots; queued demand preserves older pinned tag, label, and execution-profile targets.",
    "Expand excluded pools to see whether workspace scope, drain state, live capacity, tag, label, or execution-profile matching prevents a new claim.",
    "Build drift is shown separately and never becomes an implicit placement rule.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click(".tabBar .tab[href$='/placement']");
    await page.waitForSelector(".routingActionTable");
    await page.click(".routingActionTable details summary");
    await capture(this.id);
  },
};
