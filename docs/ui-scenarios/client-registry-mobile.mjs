export default {
  order: 6.45,
  id: "client-registry-mobile",
  title: "Review clients on a narrow screen",
  description:
    "The compact client table keeps its information hierarchy and remains horizontally scrollable on narrow screens.",
  screenshot: "docs/assets/ui/client-registry-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open Client Registry on a narrow screen.",
    "Review the client identity and credential status first.",
    "Scroll the table horizontally when the latest change or edit action is needed.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Client Registry");
    await page.waitForSelector("#clientList tbody tr");
    await capture(this.id);
  },
};
