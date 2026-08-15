import { describe, expect, test } from "vitest";
import type { ActionView } from "../lib/api";
import {
  actionExecutionLimitRows,
  concurrencyLimits,
  executionLimitRows,
  rateLimits,
} from "./ExecutionLimitsPanel";

describe("execution limits", () => {
  test("treats an omitted release policy as an empty list", () => {
    expect(concurrencyLimits(undefined)).toEqual([]);
    expect(rateLimits(undefined)).toEqual([]);
    expect(executionLimitRows(undefined)).toEqual([]);
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
        kind: "concurrency",
        limit: {
          id: "earlier",
          max_concurrent: 1,
          input_pointers: ["/account_id"],
        },
      },
      {
        actionKey: "10",
        actionName: "Tenth Action",
        kind: "concurrency",
        limit: {
          id: "later",
          max_concurrent: 1,
          input_pointers: ["/account_id"],
        },
      },
    ]);
  });

  test("projects concurrency and rate declarations without mixing their semantics", () => {
    expect(
      executionLimitRows({
        concurrency: [{ id: "parallel", max_concurrent: 2, input_pointers: ["/account_id"] }],
        rate: [
          {
            id: "vendor-rate",
            max_attempts: 120,
            window_seconds: 60,
            input_pointers: ["/account_id", "/egress/id"],
          },
        ],
      }),
    ).toEqual([
      {
        kind: "concurrency",
        limit: { id: "parallel", max_concurrent: 2, input_pointers: ["/account_id"] },
      },
      {
        kind: "rate",
        limit: {
          id: "vendor-rate",
          max_attempts: 120,
          window_seconds: 60,
          input_pointers: ["/account_id", "/egress/id"],
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
