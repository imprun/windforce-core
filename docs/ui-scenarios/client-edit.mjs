export default {
  order: 6.6,
  id: "client-edit",
  title: "Edit an app caller and its API credential",
  description:
    "App-caller identity, API credential lifecycle, and deletion are separated into clear sections with one primary save action.",
  screenshot: "docs/assets/ui/client-edit.png",
  guide: [
    "Open App access and choose Edit.",
    "Update the app caller name or manage its API credential.",
    "Revoke the active credential before deleting the app caller.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("App access");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .rowActions .button");
    await page.waitForSelector("#client-edit-dialog");
    await capture(this.id);
  },
};
