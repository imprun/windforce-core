export default {
  id: "client-invocation-policy",
  title: "Set customer app access",
  description:
    "Core operators can grant every app and action or an exact selected list for each customer.",
  guide: [
    "Open Customers and select a customer.",
    "Review the effective app access, allowed identifiers, and revision.",
    "Choose Edit app access to replace the access used for new Runs.",
  ],
  screenshot: "docs/assets/ui/client-invocation-policy.png",
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Customers");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .cellTitle");
    await page.waitForSelector('[data-ui-guide="edit-client-invocation-policy"]');
    await page.click('[data-ui-guide="edit-client-invocation-policy"]');
    await page.waitForSelector("#client-invocation-policy-dialog");
    await capture("client-invocation-policy");
  },
};
