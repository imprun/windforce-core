export default {
  id: "host-console-navigation",
  title: "Return to the host console",
  description:
    "A hosted or self-managed portal can configure one explicit return action without changing the engine navigation.",
  screenshot: "docs/assets/ui/host-console-navigation.png",
  guide: [
    "Open the embedded Web UI from the configured host portal.",
    "Use Back to host console in the top bar to return to the surrounding product.",
    "On narrow screens the same action remains available as an icon with an accessible label.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("a[aria-label='Back to host console']");
    await capture();
  },
};
