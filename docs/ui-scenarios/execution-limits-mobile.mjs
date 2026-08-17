export default {
  order: 3.07,
  id: "execution-limits-mobile",
  title: "Review execution limits on a narrow screen",
  description:
    "Policy tables become labelled records so ceiling, allowance, effective value, and actions remain usable on a narrow screen.",
  screenshot: "docs/assets/ui/execution-limits-mobile.png",
  viewport: { width: 390, height: 1500 },
  guide: [
    "Open an App and choose 앱 설정, then 실행 제한 on a narrow screen.",
    "Review App-wide and Action-specific limits as labelled records.",
    "Apply or remove an operating allowance without horizontal table scrolling.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click('[data-ui-guide="app-settings"]');
    await page.click(".tabBar .tab[href$='/settings/execution-limits']");
    await page.waitForSelector('[data-ui-guide="execution-limit-policies"]');
    await capture(this.id);
  },
};
