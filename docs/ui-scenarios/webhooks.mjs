export default {
  order: 11,
  id: "webhooks",
  title: "Manage lifecycle webhooks",
  description:
    "The Webhooks settings view shows each signed lifecycle receiver, its app scope, and the latest delivery outcome without exposing endpoint paths or secrets.",
  screenshot: "docs/assets/ui/webhooks.png",
  guide: [
    "Open Settings and choose Webhooks.",
    "Review each receiver's status, masked endpoint, app scope, latest delivery, and last operator update.",
    "Open a webhook name to manage its configuration and delivery history.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => {
      localStorage.setItem("wf.locale", "en");
      localStorage.setItem("wf.workspace", "default");
    });
    await page.goto("/ui/settings/webhooks");
    await page.waitForSelector("#webhookList tbody tr");
    await page.waitForText("#webhookList", "Release notifications");
    await capture(this.id);
  },
};
