export default {
  id: "client-input-settings",
  title: "Manage customer input settings",
  description: "Review app- and action-specific values applied for one customer.",
  guide: [
    "Open Customers.",
    "Select a customer.",
    "Open a settings row to review its JSON values and locked keys.",
  ],
  screenshot: "docs/assets/ui/client-input-settings.png",
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Customers");
    await page.clickText("Example Retailer");
    await page.clickText("Input Settings");
    await page.waitForSelector("#clientInputSettingsSummary tbody tr");
    await page.click("#clientInputSettingsSummary tbody a");
    await page.waitForSelector("#clientInputSettings .inputSettingScope");
    await page.click('#clientInputSettings button[aria-label^="Edit"]');
    await page.waitForSelector('input[value="response_mode"]');
    await page.waitForSelector('button[aria-label="Unlock input key"]');
    await capture("client-input-settings");
  },
};
