export default {
  id: "host-console-navigation",
  title: "Open host management",
  description:
    "A hosted or self-managed portal can configure one explicit management action in the sidebar footer without crowding the runtime top bar.",
  screenshot: "docs/assets/ui/host-console-navigation.png",
  guide: [
    "Open the embedded Web UI from the configured host portal.",
    "Use the configured host action above the account control in the sidebar footer.",
    "The destination label makes the management-plane boundary explicit.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("[data-testid='host-console-action']");
    await capture();
  },
};
