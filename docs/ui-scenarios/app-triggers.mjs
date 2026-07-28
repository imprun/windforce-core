export default {
  order: 7,
  id: "app-triggers",
  title: "Manage App Triggers",
  description:
    "The App Triggers tab keeps inbound Webhook, Schedule, and RabbitMQ sources beside the Action contract they invoke.",
  screenshot: "docs/assets/ui/app-triggers.png",
  guide: [
    "Open an App and choose Triggers.",
    "Review each source kind, target Action, enablement state, and latest delivery outcome.",
    "Enable or disable a source without changing its configuration.",
    "Keep outbound release notifications in Settings → Webhooks.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.clickText("Triggers");
    await page.waitForSelector("#appTriggers tbody tr");
    await capture(this.id);
  },
};
