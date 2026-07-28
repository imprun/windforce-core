export default {
  order: 8,
  id: "trigger-create",
  title: "Add an inbound Trigger",
  description:
    "A kind-aware editor configures the current App target and an explicit completion output while keeping signing secrets and broker credentials write-only.",
  screenshot: "docs/assets/ui/trigger-create.png",
  guide: [
    "Choose Add trigger from the App Triggers tab.",
    "Select Webhook, Schedule, or RabbitMQ and a target Action.",
    "Choose Poll, signed HTTP callback, RabbitMQ publish, or deliberate No output.",
    "Complete the kind-specific delivery and security fields.",
    "Create the Trigger disabled, verify it, and enable it from the list.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.clickText("Triggers");
    await page.waitForSelector("#appTriggers tbody tr");
    await page.click("#addTriggerButton");
    await page.waitForSelector("#triggerEditorDialog");
    await page.click("#triggerCompletionMode");
    await page.waitForSelector(".selectContent");
    await capture(this.id);
    await page.click("#triggerCompletionMode");
    await page.fill("#triggerName", "UI guide schedule");
    await page.click("#triggerKind-schedule");
    await page.clickText("Create trigger");
    await page.waitForText("#appTriggers", "UI guide schedule");
  },
};
