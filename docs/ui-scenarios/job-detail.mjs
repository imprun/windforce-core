export default {
  order: 8,
  id: "job-detail",
  title: "Inspect a job",
  description:
    "The job detail view shows one run end to end: identity and timing, the recorded input, the result envelope, and the log tail.",
  screenshot: "docs/assets/ui/job-detail.png",
  guide: [
    "Open a job from the Jobs view.",
    "Check status, timing, worker, release commit, and audit actor.",
    "Inspect the recorded input and the action result, or the failure envelope on errors.",
    "Read the stdout/stderr tail; unsettled jobs refresh automatically and can be canceled.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Jobs");
    await page.waitForSelector("#jobList .tableRow");
    await page.click("#jobList .tableRow");
    await page.waitForText("h2", "Run");
    await capture(this.id);
  },
};
