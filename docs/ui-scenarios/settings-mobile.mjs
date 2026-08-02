export default {
  order: 9.1,
  id: "settings-mobile",
  title: "Manage the active workspace on mobile",
  description:
    "Workspace identity, API access, Core connection details, and lifecycle controls remain usable on a narrow screen.",
  screenshot: "docs/assets/ui/settings-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open Settings and choose Workspace.",
    "Issue a named workspace token without horizontal page overflow.",
    "Continue through Core connection and lifecycle sections on the same workspace page.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/workspace']");
    await page.waitForSelector(".workspaceTokenCreate");
    await page.waitForText("main", "Workspace API tokens");
    await capture(this.id);
  },
};
