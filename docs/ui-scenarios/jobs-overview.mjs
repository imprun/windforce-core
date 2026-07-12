export default {
  order: 7,
  id: "jobs-overview",
  title: "Monitor jobs",
  description:
    "The Jobs view summarizes run activity across the workspace and lists every job with its status, trigger, and actor.",
  screenshot: "docs/assets/ui/jobs-overview.png",
  guide: [
    "Open Jobs from the sidebar.",
    "Read the summary tiles: queued, running, and the last 24 hours of completed, failed, and canceled runs.",
    "Filter the list by status or app key.",
    "Open a job to inspect its input, result, and logs.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Jobs");
    await page.waitForSelector("#jobSummary");
    await page.waitForSelector("#jobList .tableRow");
    await capture(this.id);
  },
};
