export default {
  order: 6.5,
  id: "client-register",
  title: "Register an external client",
  description:
    "Client registration presents one primary identity field and explains the one-time token before creation.",
  screenshot: "docs/assets/ui/client-register.png",
  guide: [
    "Open Client Registry and choose Register Client.",
    "Enter the external client name.",
    "Create the client to receive its one-time Invocation API token.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Client Registry");
    await page.clickText("Register Client");
    await page.waitForSelector("#client-register-dialog");
    await capture(this.id);
  },
};
