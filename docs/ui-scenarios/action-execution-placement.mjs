export default {
  order: 3.2,
  id: "action-execution-placement",
  title: "Configure Action execution placement",
  description:
    "Each Action inherits App placement by default and can override its worker tag and required labels independently.",
  screenshot: "docs/assets/ui/action-execution-placement.png",
  guide: [
    "Open the app and choose the Placement tab.",
    "Choose Edit on an Action row.",
    "Keep Inherit App placement or select an Action-specific override.",
    "The release-label default combines App and Action runsOn values.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click('[data-ui-guide="app-settings"]');
    await page.click(".tabBar .tab[href$='/settings/placement']");
    await page.waitForSelector(".routingActionTable tbody tr");
    await page.click('[data-ui-guide^="edit-action-execution-placement-"]');
    await page.waitForSelector("[role=dialog]");
    await capture(this.id);
  },
};
