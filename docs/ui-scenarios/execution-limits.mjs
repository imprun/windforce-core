export default {
  order: 3.05,
  id: "execution-limits",
  title: "Review release execution limits",
  description:
    "The Execution limits tab shows immutable App and Action keyed-concurrency and fixed-window rate policies from the active release.",
  screenshot: "docs/assets/ui/execution-limits.png",
  guide: [
    "Open an App and choose Execution limits.",
    "Review App-scoped policies shared by every Action.",
    "Compare concurrency capacity and fixed-window attempt budgets without exposing resolved key values.",
    "Review Action-scoped policies and JSON Pointer key inputs.",
    "Edit the source manifest and publish a new release when the policy must change.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click(".tabBar .tab[href$='/execution-limits']");
    await page.waitForSelector('[data-ui-guide="app-execution-limits"]');
    await page.waitForSelector('[data-ui-guide="action-execution-limits"]');
    await capture(this.id);
  },
};
