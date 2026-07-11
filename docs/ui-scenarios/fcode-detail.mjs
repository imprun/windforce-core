export default {
  order: 3,
  id: "fcode-detail",
  title: "Inspect an FCode deployment detail",
  description: "Open a registered FCode detail page to review source registration, active contract, pending requests, readiness, and audit evidence.",
  screenshot: "docs/assets/ui/fcode-detail.png",
  guide: [
    "Open the deployment management console.",
    "Select a registered FCode and open its detail page.",
    "Review the worker contract and exposed actions.",
    "Check pending deployment requests and readiness signals.",
    "Inspect the active source snapshot and latest audit entries.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => {
      localStorage.setItem("wf.actor", "ui-guide@example.test");
    });
    await page.goto();
    await page.waitForSelector("#sourceDetail");
    await page.click("#openSelectedFCodeDetail");
    await page.waitForSelector("#fcodeDetailPage");
    await page.waitForText("#fcodeDetailPage", "Worker contract");
    await page.waitForText("#fcodeDetailPage", "Request history");
    await page.waitForText("#sourceSnapshot", "windforce.json");
    await capture(this.id);
  },
};
