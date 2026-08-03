export default {
  order: 12,
  id: "webhook-settings",
  title: "Configure a lifecycle webhook",
  description:
    "Webhook detail keeps event selection, receiver configuration, app scope, enablement, secret rotation, and deletion controls on a full page.",
  screenshot: "docs/assets/ui/webhook-settings.png",
  guide: [
    "Open a webhook from Settings.",
    "Review its masked receiver, selected lifecycle events, status, and last operator update.",
    "Change its event selection, name, endpoint, enablement, or app scope.",
    "Rotate the signing secret only when the receiver can be updated immediately.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => {
      localStorage.setItem("wf.locale", "en");
      localStorage.setItem("wf.workspace", "default");
    });
    await page.goto("/ui/settings/webhooks");
    await page.waitForSelector("#webhookList tbody tr");
    await page.clickText("Release notifications");
    await page.waitForSelector("#saveWebhookButton");
    await capture(this.id);
  },
};
