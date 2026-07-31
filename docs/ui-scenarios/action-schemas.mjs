export default {
  order: 6,
  id: "action-schemas",
  title: "Review action schemas",
  description:
    "The Docs tab shows each action's release-pinned input and output JSON Schemas.",
  screenshot: "docs/assets/ui/action-schemas.png",
  guide: [
    "Open an app and switch to the Docs tab.",
    "Choose an action from the API reference.",
    "Review its request and result fields or download the source JSON Schema.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.clickText("Docs");
    const actionPath = await page.evaluate(
      () => document.querySelector('a[href$="/docs/actions/echo"]')?.getAttribute("href") || "",
    );
    if (!actionPath) throw new Error("echo Action documentation link was not found");
    // Use the canonical URL so the release-pinned schema request cannot race
    // the two preceding client-side route changes.
    await page.goto(actionPath);
    await page.waitForText(".schemaSection h3", "Request body");
    await capture(this.id);
  },
};
