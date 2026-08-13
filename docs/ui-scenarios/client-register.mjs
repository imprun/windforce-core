export default {
  order: 6.5,
  id: "client-register",
  title: "Register an app caller with app access",
  description:
    "App-caller registration commits the initial app access and API credential together.",
  screenshot: "docs/assets/ui/client-register.png",
  guide: [
    "Open App access and choose Register app caller.",
    "Enter the app caller name and choose initial app access.",
    "Create the app caller to receive its one-time API credential.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("App access");
    await page.clickText("Register app caller");
    await page.waitForSelector("#client-register-dialog");
    await capture(this.id);
  },
};
