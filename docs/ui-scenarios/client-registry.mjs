export default {
  order: 6.4,
  id: "client-registry",
  title: "Manage customers",
  description:
    "The customer registry keeps credential state and the latest change compact while preserving direct access to each customer.",
  screenshot: "docs/assets/ui/client-registry.png",
  guide: [
    "Open Customers.",
    "Scan the customer name, API credential state, and latest change.",
    "Open a customer name for app access and input settings, or choose Edit for credential management.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Customers");
    await page.waitForSelector("#clientList tbody tr");
    await capture(this.id);
  },
};
