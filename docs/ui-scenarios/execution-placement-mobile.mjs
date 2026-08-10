export default {
  order: 3.12,
  id: "execution-placement-mobile",
  title: "Configure execution placement on a narrow screen",
  description:
    "The App placement summary and editor collapse to one column without hiding worker selection or save controls.",
  screenshot: "docs/assets/ui/execution-placement-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open an App and choose Placement on a narrow screen.",
    "Open the App placement editor.",
    "Review worker tag and required-label controls in the one-column layout.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click(".tabBar .tab[href$='/placement']");
    await page.waitForSelector(".routingPolicySummary");
    await page.click('[data-ui-guide="edit-app-execution-placement"]');
    await page.waitForSelector("[role=dialog]");
    await capture(this.id);
  },
};
