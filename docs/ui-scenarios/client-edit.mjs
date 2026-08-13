export default {
  order: 6.6,
  id: "client-edit",
  title: "Edit a customer and its API credential",
  description:
    "Customer identity, API credential lifecycle, and deletion are separated into clear sections with one primary save action.",
  screenshot: "docs/assets/ui/client-edit.png",
  guide: [
    "Open Customers and choose Edit.",
    "Update the customer name or manage its API credential.",
    "Revoke the active credential before deleting the customer.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Customers");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .rowActions .button");
    await page.waitForSelector("#client-edit-dialog");
    await capture(this.id);
  },
};
