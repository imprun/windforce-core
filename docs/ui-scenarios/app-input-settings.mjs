export default {
  order: 3.7,
  id: "app-input-settings",
  title: "Manage app-caller input settings from an app",
  description:
    "App and action schemas stay in context while an operator reviews values for one app caller.",
  guide: [
    "Open an app and choose App settings, then Input Settings.",
    "Select an app-caller scope.",
    "Open a settings row to review its JSON values and locked keys.",
  ],
  screenshot: "docs/assets/ui/app-input-settings.png",
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click('[data-ui-guide="app-settings"]');
    await page.clickText("Input Settings");
    await page.waitForSelector("#appInputSettingsSummary tbody tr");
    await page.clickText("Example Retailer");
    await page.waitForSelector("#appInputSettings .inputSettingScope");
    await page.click('#appInputSettings button[aria-label^="Edit"]');
    await page.waitForSelector('input[value="response_mode"]');
    await page.waitForSelector('button[aria-label="Unlock input key"]');
    await capture(this.id);
  },
};
