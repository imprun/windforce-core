export default {
  order: 6.7,
  id: "client-token-confirmation",
  title: "Confirm an irreversible token action",
  description:
    "Token rotation uses the shared in-product confirmation dialog instead of a browser-native prompt.",
  screenshot: "docs/assets/ui/client-token-confirmation.png",
  guide: [
    "Open a client for editing.",
    "Choose Rotate token.",
    "Review the immediate invalidation warning before confirming or canceling.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Client Registry");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .rowActions .button");
    await page.waitForSelector("#client-edit-dialog");
    await page.clickText("Rotate token");
    await page.waitForSelector(".dialog.compact");
    await capture(this.id);
  },
};
