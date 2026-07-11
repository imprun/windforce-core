export default {
  id: "app-action-schema",
  title: "Browse app action schemas",
  description: "Use the Apps view to inspect synced action contracts and rendered JSON schemas.",
  screenshot: "docs/assets/ui/app-action-schema.png",
  guide: [
    "Open the Apps view.",
    "Select a synced app.",
    "Select an action to load input and output schemas.",
    "Use the Schema, History, and Source tabs to inspect the deployed contract.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Apps");
    await page.waitForSelector("#actionList [data-action]");
    await page.click("#actionList [data-action]");
    await page.waitForText("#schemaTab", "input_schema");
    await capture(this.id);
  },
};
