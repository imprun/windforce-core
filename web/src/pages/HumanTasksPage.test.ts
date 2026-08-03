import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const source = await readFile(new URL("./HumanTasksPage.tsx", import.meta.url), "utf8");

describe("HumanTasksPage", () => {
  test("polls the durable queue and keeps decision and cancel separate", () => {
    expect(source).toContain("window.setInterval(state.reload, 5000)");
    expect(source).toContain('decide(selected, "submit", value)');
    expect(source).toContain('await decide(task, "cancel")');
    expect(source).toContain("<ConfirmDialog");
  });

  test("never renders private context and uses the shared Radix-backed controls", () => {
    expect(source).not.toContain("task.private_context");
    expect(source).toContain("<SelectControl");
    expect(source).toContain("<Modal");
  });
});
