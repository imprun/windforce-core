export default {
  order: 8,
  id: "job-log-inspector",
  title: "Follow one Job's logs",
  description:
    "The focused inspector follows masked stdout and stderr for a known Job ID while Monitoring remains aggregate-first.",
  screenshot: "docs/assets/ui/job-log-inspector.png",
  guide: [
    "Open Monitoring and choose Inspect job logs.",
    "Paste the Job ID obtained from an invocation response, alert, or CLI query.",
    "Connect to replay retained bytes and continue following from the latest byte offset.",
    "Use status, App/Action, Worker, attempt, release commit, and start time to anchor the diagnosis.",
    "Review the policy ID, scope, capacity, revision, and opaque key digest pinned at admission.",
  ],
  async run({ page, capture, api }) {
    const jobs = await api("/jobs?status=all&limit=10");
    const job = jobs.items.find((item) => item.app_key === "echo") || jobs.items[0];
    if (!job) throw new Error("job log inspector scenario requires a seeded Job");

    await page.goto("monitoring");
    await page.click("#openJobLogInspector");
    await page.waitForSelector("#jobLogInspector");
    await page.fill("#jobLogInspector input", job.id);
    await page.click("#connectJobLogs");
    await page.waitForSelector("#jobLogInspector .jobLogFacts");
    await page.waitForSelector("#jobLogInspector .jobLogOutput");
    await capture(this.id);
  },
};
