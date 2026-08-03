export default {
  order: 7.6,
  id: "human-task-decision",
  title: "Submit a HumanTask decision",
  description:
    "The generic decision dialog renders supported JSON Schema fields while keeping private context hidden.",
  screenshot: "docs/assets/ui/human-task-decision.png",
  guide: [
    "Choose Resolve on a pending HumanTask.",
    "Confirm the target Action and deadline before entering values.",
    "Fill the schema-driven form; private context remains a presence indicator only.",
    "Submit the decision or use the separately confirmed Cancel task action.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.evaluate(() => {
      const originalFetch = window.fetch.bind(window);
      window.fetch = async (input, init) => {
        const url = String(input);
        if (url.includes("/api/w/default/human-tasks?")) {
          return new Response(JSON.stringify({
            items: [{
              id: "human_01JHOLDDEMO",
              workspace_id: "default",
              run_id: "run_01JHOLDDEMO",
              job_id: "job_01JHOLDDEMO",
              attempt: 1,
              app: "partner-portal",
              action: "collect-orders",
              key: "login-otp",
              mode: "hold",
              kind: "form",
              state: "pending",
              title: "Enter the verification code",
              description: "Use the code sent to the account owner.",
              input_schema: {
                type: "object",
                required: ["otp"],
                properties: {
                  otp: { type: "string", title: "Verification code", description: "Six digits" },
                  remember: { type: "boolean", title: "Remember this browser" },
                },
              },
              has_private_context: true,
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
              expires_at: new Date(Date.now() + 120_000).toISOString(),
            }],
          }), { status: 200, headers: { "content-type": "application/json" } });
        }
        return originalFetch(input, init);
      };
    });
    await page.click("a[href$='/human-tasks']");
    await page.waitForSelector(".humanTaskTable tbody tr");
    await page.clickText("Resolve");
    await page.waitForSelector("#humanTaskDialog");
    await capture(this.id);
  },
};
