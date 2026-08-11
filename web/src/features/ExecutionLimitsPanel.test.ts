import { describe, expect, test } from "vitest";
import type { ActionView } from "../lib/api";
import { actionExecutionLimitRows, concurrencyLimits } from "./ExecutionLimitsPanel";

describe("execution limits", () => {
  test("treats an omitted release policy as an empty list", () => {
    expect(concurrencyLimits(undefined)).toEqual([]);
  });

  test("sorts action policies and preserves every declared limit", () => {
    const actions: ActionView[] = [
      action("10", "Tenth Action", "later"),
      action("2", "Second Action", "earlier"),
      action("empty", "Empty Action"),
    ];

    expect(actionExecutionLimitRows(actions)).toEqual([
      {
        actionKey: "2",
        actionName: "Second Action",
        limit: {
          id: "earlier",
          max_concurrent: 1,
          input_pointers: ["/account_id"],
        },
      },
      {
        actionKey: "10",
        actionName: "Tenth Action",
        limit: {
          id: "later",
          max_concurrent: 1,
          input_pointers: ["/account_id"],
        },
      },
    ]);
  });
});

function action(actionKey: string, displayName: string, policyID?: string): ActionView {
  return {
    id: actionKey,
    workspace_id: "default",
    app_key: "echo",
    action_key: actionKey,
    display_name: displayName,
    execution_limits: policyID
      ? {
          concurrency: [
            {
              id: policyID,
              max_concurrent: 1,
              input_pointers: ["/account_id"],
            },
          ],
        }
      : undefined,
    updated_at: "2026-08-11T00:00:00Z",
  };
}
