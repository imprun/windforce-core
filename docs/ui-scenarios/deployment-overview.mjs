export default {
  order: 2,
  id: "deployment-overview",
  title: "Review deployment requests",
  description: "Use the deployment console to review pending FCode deployment requests, registered sources, and drill into deployable detail pages.",
  screenshot: "docs/assets/ui/deployment-overview.png",
  guide: [
    "Open the deployment management console.",
    "Use the sidebar to move between deployment, source, release, and audit work areas.",
    "Use the deployment request queue to identify pending operator work.",
    "Use the release candidate table to compare registered FCodes.",
    "Select a row for quick comparison or open its detail page for deployment evidence.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForText("#requestQueue", "Deployment requests");
    await page.waitForText("#sourceList", "FCode release candidates");
    await page.waitForSelector("#requestQueue .tableRow");
    await page.waitForSelector("#sourceList .tableRow");
    await page.waitForSelector("#sourceDetail");
    await capture(this.id);
  },
};
