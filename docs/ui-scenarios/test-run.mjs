export default {
  order: 6,
  id: "test-run",
  title: "Test run an action",
  description:
    "The Actions tab shows each action's materialized JSON Schemas and lets you run the action against the active contract.",
  screenshot: "docs/assets/ui/test-run.png",
  guide: [
    "Open an app and switch to the Actions tab.",
    "Review the input and output JSON Schemas materialized from the release.",
    "Edit the input JSON and click Run action.",
    "The run result appears inline with a link to the full job record.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .tableRow");
    await page.clickText("Actions");
    await page.waitForText("h2", "Action · echo");
    await page.fill(".testRun textarea", '{"message": "hello from the UI guide"}');
    await page.clickText("Run action");
    await page.waitForSelector(".testRunActions .badge");
    await capture(this.id);
  },
};
