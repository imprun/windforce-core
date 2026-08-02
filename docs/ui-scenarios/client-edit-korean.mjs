export default {
  order: 6.65,
  id: "client-edit-korean",
  title: "Edit a client in Korean",
  description:
    "The client identity, token lifecycle, and danger zone preserve their hierarchy with Korean labels.",
  screenshot: "docs/assets/ui/client-edit-korean.png",
  guide: [
    "Switch the console to Korean and open API clients.",
    "Choose Edit for a client.",
    "Confirm the token status, actions, and deletion guidance remain readable without crowding.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto();
    await page.clickText("API 클라이언트");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .rowActions .button");
    await page.waitForSelector("#client-edit-dialog");
    await capture(this.id);
  },
};
