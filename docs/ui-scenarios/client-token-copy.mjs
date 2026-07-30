export default {
  order: 6.8,
  id: "client-token-copy",
  title: "Copy a newly issued client token",
  description:
    "The one-time token stays in a stable copy row and the copy button itself confirms success.",
  screenshot: "docs/assets/ui/client-token-copy.png",
  guide: [
    "Register a client to issue its one-time token.",
    "Copy the token into the calling system.",
    "Confirm the button changes to Copied before closing the dialog.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Client Registry");
    await page.clickText("Register Client");
    await page.fill("#clientName", "Capture Partner");
    await page.clickText("Create client");
    await page.waitForSelector("#client-token-dialog");
    await page.clickText("Copy token");
    await page.waitForText("#client-token-dialog", "Copied");
    await page.evaluate(() => {
      const input = document.querySelector("#client-token-dialog input");
      if (input instanceof HTMLInputElement) input.value = "wfk_REDACTED_FOR_DOCUMENTATION";
    });
    await capture(this.id);
  },
};
