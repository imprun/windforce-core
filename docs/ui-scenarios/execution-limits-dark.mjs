export default {
  order: 3.06,
  id: "execution-limits-dark",
  title: "Review execution limits in dark mode",
  description:
    "Dark mode preserves policy hierarchy, identifiers, and diagnostic boundaries without introducing a separate palette.",
  screenshot: "docs/assets/ui/execution-limits-dark.png",
  guide: [
    "Open an App and choose Execution limits.",
    "Switch the Core console to dark mode.",
    "Confirm policy IDs, capacities, JSON Pointers, and release guidance remain legible.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click(".tabBar .tab[href$='/execution-limits']");
    await page.waitForSelector('[data-ui-guide="app-execution-limits"]');
    await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
    await capture(this.id);
  },
};
