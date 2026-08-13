export default {
  id: "client-invocation-policy",
  title: "Limit client invocation access",
  description:
    "Core operators can choose broad compatibility or an exact App and Action allow-list for each client.",
  guide: [
    "Open Client Registry and select a client.",
    "Review the effective policy, targets, and revision.",
    "Choose Edit access to replace the policy for newly admitted Runs.",
  ],
  screenshot: "docs/assets/ui/client-invocation-policy.png",
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Client Registry");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .cellTitle");
    await page.waitForSelector('[data-ui-guide="edit-client-invocation-policy"]');
    await page.click('[data-ui-guide="edit-client-invocation-policy"]');
    await page.waitForSelector("#client-invocation-policy-dialog");
    await capture("client-invocation-policy");
  },
};
