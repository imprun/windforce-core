export default {
  order: 2,
  id: "register-app",
  title: "Register an app",
  description:
    "Register App validates the configured canonical manifest, previews its App identity and execution-placement defaults, and can store an initial operator policy.",
  screenshot: "docs/assets/ui/register-app.png",
  guide: [
    "Click Register App in the Apps view.",
    "Enter the app name, repository URL, branch, and optional subpath.",
    "Pick a git auth method or reference an existing credential variable path.",
    "Use Probe repository to confirm reachability and preview the configured canonical manifest.",
    "Optionally override the release worker tag or required labels. Core stores these values independently from later Releases.",
  ],
  async run({ page, capture, api }) {
    const sources = await api("/git_sources");
    const sample = sources.find((source) => source.name === "echo-service") || sources[0];
    if (!sample) throw new Error("register app scenario requires a seeded git source");
    await page.goto();
    await page.waitForSelector("#registerAppButton");
    await page.click("#registerAppButton");
    await page.waitForSelector("#registerAppDialog");
    await page.fill("#registerAppDialog input[placeholder='echo']", "orders");
    await page.fill(
      "#registerAppDialog input[placeholder='https://github.com/org/repo.git']",
      sample.repo_url,
    );
    await page.click('[data-ui-guide="probe-app-repository"]');
    await page.waitForSelector(".registrationRoutingPolicy");
    await capture(this.id);
  },
};
