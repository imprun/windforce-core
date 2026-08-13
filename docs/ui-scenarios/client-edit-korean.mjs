export default {
  order: 6.65,
  id: "client-edit-korean",
  title: "Edit an app caller in Korean",
  description:
    "App-caller identity, API credential lifecycle, and the danger zone preserve their hierarchy with Korean labels.",
  screenshot: "docs/assets/ui/client-edit-korean.png",
  guide: [
    "Switch the console to Korean and open App access.",
    "Choose Edit for an app caller.",
    "Confirm the credential status, actions, and deletion guidance remain readable without crowding.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto();
    await page.clickText("앱 이용 권한");
    await page.waitForSelector("#clientList tbody tr");
    await page.click("#clientList tbody .rowActions .button");
    await page.waitForSelector("#client-edit-dialog");
    await capture(this.id);
  },
};
