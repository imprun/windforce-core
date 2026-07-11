export default {
  order: 4,
  id: "deployment-request-detail",
  title: "Inspect a pending deployment request detail",
  description: "Open a pending deployment request detail page to review the pinned commit, timeline, requester message, operator decision state, and related FCode.",
  screenshot: "docs/assets/ui/deployment-request-detail.png",
  guide: [
    "Open the deployment management console.",
    "Open a pending deployment request detail page from the queue.",
    "Confirm the request timeline and target commit.",
    "Review requester and operator notes.",
    "Open the related FCode detail when source-level evidence is needed.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => {
      localStorage.setItem("wf.actor", "ui-guide@example.test");
    });
    await page.goto();
    await page.waitForSelector("#requestQueue .tableRow");
    await page.click("#requestQueue .rowButtons .button");
    await page.waitForSelector("#requestDetailPage");
    await page.waitForText("#requestDetailPage", "Request timeline");
    await page.waitForText("#requestDetailPage", "Request audit");
    await page.waitForText("#requestDetailPage", "Target release");
    await capture(this.id);
  },
};
