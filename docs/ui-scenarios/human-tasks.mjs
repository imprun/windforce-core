export default {
  order: 7.5,
  id: "human-tasks",
  title: "Resolve a HumanTask hold",
  description:
    "The workspace queue shows generic requests that keep their original Action process and browser session alive while waiting for a decision.",
  screenshot: "docs/assets/ui/human-tasks.png",
  guide: [
    "Open Human tasks from the workspace sidebar.",
    "Review the request, target Action, state, and deadline without exposing private context.",
    "Open Resolve, fill the JSON Schema form, and submit one idempotent decision.",
    "Use Cancel task only when the waiting Action should receive a canceled HumanTask outcome.",
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
                properties: { otp: { type: "string", title: "Verification code" } },
              },
              has_private_context: true,
              created_at: "2026-08-03T12:00:00Z",
              updated_at: "2026-08-03T12:00:00Z",
              expires_at: new Date(Date.now() + 120_000).toISOString(),
            }],
          }), { status: 200, headers: { "content-type": "application/json" } });
        }
        return originalFetch(input, init);
      };
    });
    await page.click("a[href$='/human-tasks']");
    await page.waitForSelector(".humanTaskTable tbody tr");
    await capture(this.id);
  },
};
