export default {
  id: "job-run",
  title: "Run an action and inspect the result",
  description: "Use the Jobs view to submit JSON input, wait for execution, and inspect result/log panels.",
  screenshot: "docs/assets/ui/job-run.png",
  guide: [
    "Open the Jobs view.",
    "Choose an app and action.",
    "Enter JSON input and keep Wait for result enabled for synchronous feedback.",
    "Run the job, then inspect Result and Logs.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Jobs");
    await page.waitForText("#runApp", "echo");
    await page.waitForText("#runAction", "echo");
    await page.evaluate(() => {
      document.querySelector("#runApp").value = "echo";
      document.querySelector("#runAction").value = "echo";
      document.querySelector("#runWait").checked = true;
    });
    await page.fill("#runInput", JSON.stringify({ message: "guide-demo" }, null, 2));
    let succeeded = false;
    for (let attempt = 0; attempt < 5; attempt += 1) {
      await page.evaluate(() => {
        document.querySelector("#jobResult").textContent = "";
      });
      await page.click("#runForm .button.primary");
      await page.waitForText("#jobResult", "\"status\":");
      succeeded = await page.evaluate(() => document.querySelector("#jobResult")?.textContent.includes("\"status\": \"success\""));
      if (succeeded) break;
      await page.evaluate((delayMs) => new Promise((resolve) => setTimeout(resolve, delayMs)), 500 * (attempt + 1));
    }
    if (!succeeded) throw new Error("job did not reach success state");
    await page.evaluate(() => {
      const result = document.querySelector("#jobResult");
      if (result) {
        result.textContent = JSON.stringify({
          job_id: "00000000-0000-4000-8000-000000000000",
          result: {
            ok: true,
            app: "echo",
            action: "echo",
            input: { message: "guide-demo" },
          },
          status: "success",
        }, null, 2);
      }
      const list = document.querySelector("#jobList");
      if (list) {
        list.innerHTML = `
          <div class="table-row">
            <div>
              <div class="row-title">echo.echo</div>
              <div class="row-meta">00000000-0000-4000-8000-000000000000</div>
            </div>
            <div><span class="pill ok">success</span></div>
            <div class="row-meta">api - generated guide fixture</div>
            <div class="form-actions">
              <button class="button" type="button">Open</button>
              <button class="button" type="button">Logs</button>
            </div>
          </div>`;
      }
    });
    await capture(this.id);
  },
};
