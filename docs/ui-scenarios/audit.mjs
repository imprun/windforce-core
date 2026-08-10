export default {
  order: 7,
  id: "audit",
  title: "Audit configuration changes",
  description:
    "The Audit tab records who changed the app's configuration: repository settings edits, source deletion, and execution-placement overrides. Releases have their own history on the Releases tab.",
  screenshot: "docs/assets/ui/audit.png",
  guide: [
    "Open an app and switch to the Audit tab.",
    "Each record shows the actor, the kind of change, and the changed fields.",
    "Use it together with the Releases tab to answer who changed what, and when.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.evaluate(async () => {
      const response = await fetch("/api/w/default/apps/echo/actions/echo", {
        method: "PATCH",
        headers: {
          "Content-Type": "application/json",
          "X-Windforce-Actor": "ui-guide@example.test",
        },
        body: JSON.stringify({
          tag_override: "browser",
          required_labels_override: [],
        }),
      });
      if (!response.ok) throw new Error(`placement audit fixture failed: ${response.status}`);
    });
    await page.waitForSelector(".tabBar");
    await page.click(".tabBar .tab[href$='/audit']");
    await page.waitForSelector("#auditEvents .cellTitle");
    await capture(this.id);
  },
};
