export default {
  order: 6,
  id: "deployed-request-detail",
  title: "Inspect a deployed request detail",
  description: "Open a deployed request detail page to verify the operator decision, deployment id, published commit, and audit evidence.",
  screenshot: "docs/assets/ui/deployed-request-detail.png",
  guide: [
    "Open the deployment management console after a request has been approved.",
    "Open the deployed request detail page.",
    "Confirm the deployed state, deployment id, target commit, and operator note.",
    "Use copy actions when tracing the request through logs or release history.",
  ],
  async run({ page, capture, api }) {
    const requests = await api("/deployment_requests");
    const request = requests.requests.find((item) => item.status === "deployed");
    if (!request) throw new Error("deployed request not found");
    await openRequestDetail(page, request.id);
    await page.waitForText("#requestDetailPage", "deployed");
    await page.waitForText("#requestDetailPage", "Deployed");
    await capture(this.id);
  },
};

async function openRequestDetail(page, requestID) {
  await page.goto();
  await page.evaluate(() => {
    localStorage.setItem("wf.actor", "ui-guide@example.test");
  });
  await page.goto();
  await page.evaluate((id) => {
    const url = new URL(window.location.href);
    url.searchParams.set("detail", "request");
    url.searchParams.set("id", id);
    history.pushState(null, "", `${url.pathname}${url.search}${url.hash}`);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, requestID);
  await page.waitForSelector("#requestDetailPage");
}
