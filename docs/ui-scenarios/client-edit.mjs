export default {
  order: 6.6,
  id: "client-edit",
  title: "Edit a client and its token",
  description:
    "Client identity, token lifecycle, and deletion are separated into clear sections with one primary save action.",
  screenshot: "docs/assets/ui/client-edit.png",
  guide: [
    "Open Client Registry and choose Edit.",
    "Update the client name or manage its Invocation API token.",
    "Revoke the active token before deleting the client.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Client Registry");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .rowActions .button");
    await page.waitForSelector("#client-edit-dialog");
    await capture(this.id);
  },
};
