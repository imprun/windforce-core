export default {
  order: 6.5,
  id: "client-register",
  title: "Register a customer with app access",
  description:
    "Customer registration commits the initial app access and API credential together.",
  screenshot: "docs/assets/ui/client-register.png",
  guide: [
    "Open Customers and choose Register Customer.",
    "Enter the customer name and choose initial app access.",
    "Create the customer to receive its one-time API credential.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Customers");
    await page.clickText("Register Customer");
    await page.waitForSelector("#client-register-dialog");
    await capture(this.id);
  },
};
