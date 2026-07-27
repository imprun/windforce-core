export default {
  order: 7.5,
  id: "app-triggers-dark",
  title: "Review App Triggers in dark mode",
  description:
    "Dark mode preserves clear separation between Trigger kinds, enablement, delivery outcomes, and destructive actions.",
  screenshot: "docs/assets/ui/app-triggers-dark.png",
  guide: [
    "Switch the execution workspace to dark mode.",
    "Open an App and choose Triggers.",
    "Confirm status text and icons remain distinguishable without relying on color alone.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.clickText("Triggers");
    await page.waitForSelector("#appTriggers tbody tr");
    await page.evaluate(() => {
      document.documentElement.setAttribute("data-theme", "dark");
    });
    await capture(this.id);
  },
};
