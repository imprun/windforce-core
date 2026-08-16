export default {
  order: 3.05,
  id: "execution-limits",
  title: "Adjust execution limits",
  description:
    "The Execution limits tab separates Release safety ceilings, Cell operating allowances, and effective claim-time values.",
  screenshot: "docs/assets/ui/execution-limits.png",
  guide: [
    "Open an App and choose Execution limits.",
    "Compare the Release safety ceiling, operating allowance, and effective value.",
    "Enter a positive allowance to lower this Cell's capacity without publishing a Release.",
    "Use Release default to remove the allowance without disabling the Release safety limit.",
    "Review previous-Release cohorts separately after rollback.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click(".tabBar .tab[href$='/execution-limits']");
    await page.waitForSelector('[data-ui-guide="execution-limit-summary"]');
    await page.waitForSelector('[data-ui-guide="execution-limit-policies"]');
    await capture(this.id);
  },
};
