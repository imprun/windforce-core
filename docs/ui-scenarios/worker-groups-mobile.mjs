export default {
  order: 2.52,
  id: "worker-groups-mobile",
  title: "Review execution pools on a narrow screen",
  description:
    "Queue pressure and slot usage stay visible on a narrow screen while pinned demand and pool inventory remain available in horizontal tables.",
  screenshot: "docs/assets/ui/worker-groups-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open Execution pools from the mobile navigation.",
    "Review queued Runs and slot usage first, then scroll pinned demand and pool inventory horizontally when needed.",
  ],
  async run({ page, capture }) {
    await page.goto("execution-pools");
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto("execution-pools");
    await page.waitForSelector('[data-ui-guide="execution-demand"]');
    await capture(this.id);
  },
};
