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
    const fieldLayout = await page.evaluate(() => {
      const name = document.querySelector("#triggerName");
      const target = document.querySelector('[aria-label="Target Action"]');
      if (!(name instanceof HTMLElement) || !(target instanceof HTMLElement)) return null;
      const targetValue = target.querySelector(".selectValue");
      const nameRect = name.getBoundingClientRect();
      const targetRect = target.getBoundingClientRect();
      return {
        topDelta: Math.abs(nameRect.top - targetRect.top),
        leftDelta: Math.abs(nameRect.left - targetRect.left),
        widthDelta: Math.abs(nameRect.width - targetRect.width),
        heightDelta: Math.abs(nameRect.height - targetRect.height),
        stacked: targetRect.top >= nameRect.bottom,
        targetWraps:
          targetValue instanceof HTMLElement
            ? targetValue.scrollHeight > targetValue.clientHeight
            : null,
        targetHTML: targetValue ? "" : target.innerHTML,
      };
    });
    if (
      !fieldLayout ||
      fieldLayout.heightDelta > 1 ||
      fieldLayout.targetWraps !== false ||
      (fieldLayout.stacked
        ? fieldLayout.leftDelta > 1 || fieldLayout.widthDelta > 1
        : fieldLayout.topDelta > 1)
    ) {
      throw new Error(`Trigger identity fields are misaligned: ${JSON.stringify(fieldLayout)}`);
    }
    await page.click("#triggerCompletionMode");
    await page.waitForSelector(".selectContent");
    const overlayLayers = await page.evaluate(() => {
      const dialog = document.querySelector(".dialog");
      const select = document.querySelector(".selectContent");
      if (!(dialog instanceof HTMLElement) || !(select instanceof HTMLElement)) return null;
      return {
        dialog: Number.parseInt(getComputedStyle(dialog).zIndex || "0", 10),
        select: Number.parseInt(getComputedStyle(select).zIndex || "0", 10),
      };
    });
    if (!overlayLayers || overlayLayers.select <= overlayLayers.dialog) {
      throw new Error(`Select overlay is not above the dialog: ${JSON.stringify(overlayLayers)}`);
    }
    await capture(this.id);
    await page.click("#triggerCompletionMode");
    await page.fill("#triggerName", "UI guide schedule");
    await page.click("#triggerKind-schedule");
    await page.clickText("Create trigger");
    await page.waitForText("#appTriggers", "UI guide schedule");
  },
};
