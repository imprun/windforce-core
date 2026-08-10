export default {
  order: 3.11,
  id: "execution-placement-dark",
  title: "Configure execution placement in dark mode",
  description:
    "Dark mode keeps release defaults, operator overrides, and effective worker placement visually distinct.",
  screenshot: "docs/assets/ui/execution-placement-dark.png",
  guide: [
    "Open an App and choose Placement.",
    "Switch the console to dark mode and open the App placement editor.",
    "Confirm defaults, overrides, previews, and readiness warnings remain legible.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click(".tabBar .tab[href$='/placement']");
    await page.waitForSelector(".routingPolicySummary");
    await page.evaluate(() => {
      document.documentElement.setAttribute("data-theme", "dark");
    });
    await page.click('[data-ui-guide="edit-app-execution-placement"]');
    await page.waitForSelector("[role=dialog]");
    await capture(this.id);
  },
};
