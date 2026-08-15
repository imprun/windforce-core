export default {
  order: 2.51,
  id: "worker-groups-dark",
  title: "Review WorkerGroups in dark mode",
  description:
    "Dark mode keeps queue pressure, slot usage, pinned targets, and group diagnostics legible without changing the workspace-scoped projection.",
  screenshot: "docs/assets/ui/worker-groups-dark.png",
  guide: [
    "Open Worker groups and switch the console to dark mode.",
    "Confirm queued demand, capacity badges, and runtime metadata remain readable.",
  ],
  async run({ page, capture }) {
    await page.goto("worker-groups");
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto("worker-groups");
    await page.waitForSelector('[data-ui-guide="execution-demand"]');
    await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
    await capture(this.id);
  },
};
