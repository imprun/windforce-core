export default {
  order: 1,
  id: "apps-overview",
  title: "Review registered apps",
  description:
    "The Apps view is the home screen. Every row is one app: its release state, repository source, last release, and route tag.",
  screenshot: "docs/assets/ui/apps-overview.png",
  guide: [
    "Open the Web UI; the Apps view lists every registered app.",
    "Check the release state badge: released apps have a worker-visible contract, registered apps do not yet.",
    "Compare repository source, last release commit, action count, and route tag per app.",
    "Use Publish Release directly from a row, or Open App for the full detail view.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await capture(this.id);
  },
};
