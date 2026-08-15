export default {
  order: 2.5,
  id: "worker-groups",
  title: "Review workspace execution pools",
  description:
    "Execution pools compare queued execution demand with redacted WorkerGroup slot capacity without exposing physical Worker or credential identities.",
  screenshot: "docs/assets/ui/worker-groups.png",
  guide: [
    "Open Execution pools from the workspace navigation.",
    "Review queued Runs, oldest wait, occupied and free slots, and pinned targets before inspecting the pool inventory.",
    "A queued Run appears once even when several pools are compatible; target capacity is not summed across different targets.",
    "Treat build drift as an operational warning. It does not change placement eligibility by itself.",
    "Provisioning, scaling, and rollout remain with the configured hosting operator; this Core view is read-only.",
  ],
  async run({ page, capture }) {
    await page.goto("execution-pools");
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto("execution-pools");
    await page.waitForSelector('[data-ui-guide="execution-demand"]');
    await capture(this.id);
  },
};
