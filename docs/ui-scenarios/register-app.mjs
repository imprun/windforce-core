export default {
  order: 2,
  id: "register-app",
  title: "Register an app",
  description:
    "Register App points the control plane at a repository source. Registration validates repository access, branch, and windforce.json before saving.",
  screenshot: "docs/assets/ui/register-app.png",
  guide: [
    "Click Register App in the Apps view.",
    "Enter the app name, repository URL, branch, and optional subpath.",
    "Pick a git auth method or reference an existing credential variable path.",
    "Use Probe repository to confirm reachability and branch existence before registering.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#registerAppButton");
    await page.click("#registerAppButton");
    await page.waitForSelector("#registerAppDialog");
    await page.fill("#registerAppDialog input[placeholder='echo']", "orders");
    await page.fill(
      "#registerAppDialog input[placeholder='https://github.com/org/repo.git']",
      "https://github.com/example/orders.git",
    );
    await capture(this.id);
  },
};
