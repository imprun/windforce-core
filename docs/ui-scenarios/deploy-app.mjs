export default {
  order: 5,
  id: "deploy-app",
  title: "Approve a deployment request",
  description: "Use the Deployments view to review a developer request and publish the pinned Windforce manifest commit.",
  screenshot: "docs/assets/ui/deploy-app.png",
  guide: [
    "Open the deployment management console.",
    "Select a pending deployment request.",
    "Confirm requester, target commit, current commit, branch, and subpath.",
    "Type the FCode name and add an operator note.",
    "Approve and deploy the request to publish the active app contract.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => {
      localStorage.setItem("wf.actor", "ui-guide@example.test");
    });
    await page.goto();
    await page.waitForSelector("#requestQueue .tableRow");
    await page.click("#requestQueue .compactButton.primary");
    await page.waitForSelector("#reviewDeploymentRequestDialog");
    await page.fill("#reviewDeploymentConfirmInput", "echo");
    await page.fill("#reviewDeploymentMessage", "UI guide approval");
    await capture(this.id);
    await page.click("#reviewDeploymentRequestDialog .button.primary");
    await page.waitForText("#toast", "Deployed");
  },
};
