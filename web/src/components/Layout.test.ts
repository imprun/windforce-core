import { describe, expect, test } from "vitest";
import { primaryNavItems } from "./Layout";

describe("primaryNavItems", () => {
  test("keeps workspace administration out of workspace-scoped navigation", () => {
    expect(primaryNavItems.map((item) => item.label)).toEqual([
      "Apps",
      "Client Registry",
      "Monitoring",
      "Audit",
      "Settings",
    ]);
  });
});
