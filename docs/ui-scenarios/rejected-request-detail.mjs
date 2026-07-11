export default {
  order: 7,
  id: "rejected-request-detail",
  title: "Inspect a rejected request detail",
  description: "Open a rejected request detail page to verify rejection state, operator note, pinned commit, and source evidence.",
  screenshot: "docs/assets/ui/rejected-request-detail.png",
  guide: [
    "Create a deployment request that should not be published.",
    "Reject the request with an operator note.",
    "Open the rejected request detail page.",
    "Confirm the rejection state, requester note, operator note, and pinned target commit.",
  ],
  async run({ page, capture, api }) {
    const source = await findEchoSource(api);
    const request = await api("/deployment_requests", {
      method: "POST",
      headers: { "x-windforce-actor": "ui-guide@example.test" },
      body: {
        git_source_id: String(source.id),
        message: "UI guide rejection request",
      },
    });
    const rejected = await api(`/deployment_requests/${encodeURIComponent(request.id)}/reject`, {
      method: "POST",
      headers: { "x-windforce-actor": "ops-guide@example.test" },
      body: { message: "Rejected for UI guide audit coverage" },
    });
    await openRequestDetail(page, rejected.id);
    await page.waitForText("#requestDetailPage", "rejected");
    await page.waitForText("#requestDetailPage", "Rejected");
    await capture(this.id);
  },
};

async function findEchoSource(api) {
  const sources = await api("/git_sources");
  const source = sources.find((item) => item.name === "echo") || sources[0];
  if (!source) throw new Error("source not found");
  return source;
}

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
