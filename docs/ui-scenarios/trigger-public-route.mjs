export default {
  order: 8.5,
  id: "trigger-public-route",
  title: "Expose a Webhook Trigger through a public route",
  description:
    "A configured Router Provider reports the friendly URL and reconciliation state without replacing the canonical Trigger ingress.",
  screenshot: "docs/assets/ui/trigger-public-route.png",
  guide: [
    "Open an App, choose App settings, then Triggers, and select a Webhook Trigger.",
    "Use the canonical ingress for direct integrations in every deployment mode.",
    "When a Router Provider is configured, review the friendly public URL and its observed state.",
    "Add, edit, or delete the route independently from the Trigger definition.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click('[data-ui-guide="app-settings"]');
    await page.click(".tabBar .tab[href$='/settings/triggers']");
    await page.waitForSelector("#appTriggers tbody tr");
    await page.clickText("Partner events");
    await page.waitForSelector("#triggerDetailSheet");
    await page.waitForSelector(".triggerRouteRow");
    await capture(this.id);
  },
};
