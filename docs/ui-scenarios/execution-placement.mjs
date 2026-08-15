export default {
  order: 3.1,
  id: "execution-placement",
  title: "Configure execution placement",
  description:
    "Execution placement is operator-owned configuration that selects eligible workers and survives releases and rollbacks without changing the release manifest.",
  screenshot: "docs/assets/ui/execution-placement.png",
  guide: [
    "Open the app and choose the Placement tab.",
    "Choose whether the worker tag and required labels inherit the active release or use an operator override.",
    "An empty required-label override explicitly means no labels; Inherit follows the active release.",
    "Review the effective-after-save preview. The policy applies only to newly admitted Runs.",
    "After saving, review the server-projected eligible pools, matching Workers and slots, and any exclusion reasons for the App and each Action.",
  ],
  async run({ page, capture }) {
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
