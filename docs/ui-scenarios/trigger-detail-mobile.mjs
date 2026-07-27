export default {
  order: 9,
  id: "trigger-detail-mobile",
  title: "Inspect a Trigger on a narrow screen",
  description:
    "The Trigger detail sheet fills a narrow viewport while configuration, delivery history, and audit information remain scrollable.",
  screenshot: "docs/assets/ui/trigger-detail-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open the App Triggers tab on a narrow screen.",
    "Choose a Trigger name to open its operational detail.",
    "Review the canonical ingress and provider-managed public route without exposing secret values.",
    "Scroll to inspect delivery and audit history.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.clickText("Triggers");
    await page.waitForSelector("#appTriggers tbody tr");
    await page.clickText("Partner events");
    await page.waitForSelector("#triggerDetailSheet");
    await page.waitForSelector(".triggerRoutesSection");
    await page.evaluate(() => {
      document.querySelector(".triggerRoutesSection")?.scrollIntoView({ block: "start" });
    });
    await capture(this.id);
  },
};
