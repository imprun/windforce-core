export default {
  order: 3,
  id: "deploy-app",
  title: "Deploy an app source",
  description: "Use the Deployments view to register an app source and deploy the active Windforce manifest.",
  screenshot: "docs/assets/ui/deploy-app.png",
  guide: [
    "Open the Deployments view.",
    "Select repository access and save an access token when the repository is private.",
    "Register the Git source with branch and subpath. The deployed app key is read from the Windforce manifest.",
    "Use Deploy to validate the source, materialize the manifest, and publish the active app contract.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Deployments");
    await page.waitForSelector("#sourceList .table-row");
    await page.click("[data-sync-id]");
    await page.waitForText("#notice", "Deployed");
    await capture(this.id);
  },
};
