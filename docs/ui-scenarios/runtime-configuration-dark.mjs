export default {
  order: 9.61,
  id: "runtime-configuration-dark",
  title: "Review variables and resources in dark mode",
  description:
    "Dark mode preserves the distinction between ordinary values, write-only Secrets, tabs, and destructive actions.",
  screenshot: "docs/assets/ui/runtime-configuration-dark.png",
  guide: [
    "Switch the execution workspace to dark mode.",
    "Open Settings and choose Variables & resources.",
    "Confirm Secret status and destructive actions remain identifiable without relying on color alone.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/variables']");
    await page.waitForSelector(".runtimeConfigTable tbody tr");
    await page.evaluate(() => {
      document.documentElement.setAttribute("data-theme", "dark");
    });
    await capture(this.id);
  },
};
