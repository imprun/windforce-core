export default {
  order: 3.07,
  id: "execution-limits-mobile",
  title: "Review execution limits on a narrow screen",
  description:
    "Policy tables become labelled records on a narrow screen so scope, capacity, and key inputs remain comparable.",
  screenshot: "docs/assets/ui/execution-limits-mobile.png",
  viewport: { width: 390, height: 1100 },
  guide: [
    "Open an App and choose 실행 제한 on a narrow screen.",
    "Review the App policy as a labelled record.",
    "Continue to Action policies without horizontal table scrolling.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click(".tabBar .tab[href$='/execution-limits']");
    await page.waitForSelector('[data-ui-guide="app-execution-limits"]');
    await capture(this.id);
  },
};
