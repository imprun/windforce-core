export default {
  order: 2.5,
  id: "worker-groups",
  title: "Review workspace execution pools",
  description:
    "Execution pools show the redacted WorkerGroup capacity that Core can actually use for this workspace without exposing physical Worker or credential identities.",
  screenshot: "docs/assets/ui/worker-groups.png",
  guide: [
    "Open Execution pools from the workspace navigation.",
    "Review live Workers, free slots, current work, selectors, runtime builds, and the latest heartbeat from one Core snapshot.",
    "Treat build drift as an operational warning. It does not change placement eligibility by itself.",
    "Provisioning, scaling, and rollout remain with the configured hosting operator; this Core view is read-only.",
  ],
  async run({ page, capture }) {
    await page.goto("worker-groups");
    await page.waitForSelector('[data-ui-guide="worker-group-inventory"]');
    await capture(this.id);
  },
};
