export default {
  order: 2.52,
  id: "worker-groups-mobile",
  title: "Review execution pools on a narrow screen",
  description:
    "The execution-pool summary stays visible on a narrow screen while the detailed inventory remains available in a horizontal table.",
  screenshot: "docs/assets/ui/worker-groups-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open Execution pools from the mobile navigation.",
    "Review the capacity summary first, then scroll the detailed inventory horizontally when needed.",
  ],
  async run({ page, capture }) {
    await page.goto("worker-groups");
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto("worker-groups");
    await page.waitForSelector('[data-ui-guide="worker-group-inventory"]');
    await capture(this.id);
  },
};
