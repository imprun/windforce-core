export default {
  id: "client-invocation-policy",
  title: "Set app-caller access",
  description:
    "Core operators can grant every app and action or an exact selected list for each app caller.",
  guide: [
    "Open App access and select an app caller.",
    "Review the effective app access, allowed identifiers, and revision.",
    "Choose Edit app access to replace the access used for new Runs.",
  ],
  screenshot: "docs/assets/ui/client-invocation-policy.png",
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("App access");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .cellTitle");
    await page.waitForSelector('[data-ui-guide="edit-client-invocation-policy"]');
    await page.click('[data-ui-guide="edit-client-invocation-policy"]');
    await page.waitForSelector("#client-invocation-policy-dialog");
    await capture("client-invocation-policy");
  },
};
