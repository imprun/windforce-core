export default {
  order: 6.4,
  id: "client-registry",
  title: "Manage app access",
  description:
    "The app-access registry keeps credential state and the latest change compact while preserving direct access to each app caller.",
  screenshot: "docs/assets/ui/client-registry.png",
  guide: [
    "Open App access.",
    "Scan the app caller name, API credential state, and latest change.",
    "Open an app caller name for identity and app access, or choose Edit for credential management.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("App access");
    await page.waitForSelector("#clientList tbody tr");
    await capture(this.id);
  },
};
