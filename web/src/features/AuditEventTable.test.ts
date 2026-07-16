import { describe, expect, test } from "bun:test";
import { auditChangeGroups } from "./AuditEventTable";

describe("auditChangeGroups", () => {
  test("shows only changed setting key names in a stable order", () => {
    expect(auditChangeGroups({ updated: ["TIMEOUT"], added: ["REGION"], locked: ["REGION"] })).toEqual([
      { label: "Added", keys: ["REGION"] },
      { label: "Updated", keys: ["TIMEOUT"] },
      { label: "Locked", keys: ["REGION"] },
    ]);
  });

  test("does not invent detail for events without key changes", () => {
    expect(auditChangeGroups()).toEqual([]);
  });
});
