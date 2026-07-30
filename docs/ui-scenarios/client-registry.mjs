export default {
  order: 6.4,
  id: "client-registry",
  title: "Manage external clients",
  description:
    "The client registry keeps credential state and the latest change compact while preserving direct access to each client.",
  screenshot: "docs/assets/ui/client-registry.png",
  guide: [
    "Open Client Registry.",
    "Scan the client name, Invocation API token state, and latest change.",
    "Open a client name for input settings or choose Edit for identity and token management.",
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
