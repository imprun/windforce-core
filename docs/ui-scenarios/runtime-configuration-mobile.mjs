export default {
  order: 9.63,
  id: "runtime-configuration-mobile",
  title: "Review runtime configuration on a narrow screen",
  description:
    "The settings navigation and security notice remain readable while dense runtime data stays horizontally scrollable.",
  screenshot: "docs/assets/ui/runtime-configuration-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open Runtime configuration on a narrow screen.",
    "Switch between Variables, Resources, and Resource Types from the compact tab bar.",
    "Scroll a dense table horizontally to reach previews and row actions.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/runtime-configuration']");
    await page.waitForSelector(".runtimeConfigTable tbody tr");
    await capture(this.id);
  },
};
