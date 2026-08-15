export default {
  order: 2.51,
  id: "worker-groups-dark",
  title: "Review execution pools in dark mode",
  description:
    "Dark mode keeps capacity, drain state, selector, and build diagnostics legible without changing the workspace-scoped projection.",
  screenshot: "docs/assets/ui/worker-groups-dark.png",
  guide: [
    "Open Execution pools and switch the console to dark mode.",
    "Confirm capacity totals, status badges, and runtime metadata remain readable.",
  ],
  async run({ page, capture }) {
    await page.goto("worker-groups");
    await page.waitForSelector('[data-ui-guide="worker-group-inventory"]');
    await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
    await capture(this.id);
  },
};
