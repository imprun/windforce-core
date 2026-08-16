import { describe, expect, test } from "vitest";
import type { ActionView, ExecutionLimitPolicyReadback } from "../lib/api";
import {
  actionExecutionLimitRows,
  activePolicyForShape,
  concurrencyLimits,
  effectiveExecutionLimit,
  executionLimitRows,
  rateLimits,
  residualMatchesActiveShape,
  shortFingerprint,
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

  test("uses the lower compatible allowance while preserving an unbounded App ceiling", () => {
    expect(effectiveExecutionLimit(8, 3)).toBe(3);
    expect(effectiveExecutionLimit(3, 8)).toBe(3);
    expect(effectiveExecutionLimit(3, null)).toBe(3);
    expect(effectiveExecutionLimit(null, 4)).toBe(4);
    expect(effectiveExecutionLimit(null, null)).toBeNull();
  });

  test("matches only an applied policy with the active shape fingerprint", () => {
    const readback = executionLimitReadback();
    const shape = readback.enforced.active_release[0];
    if (!shape) throw new Error("expected an active execution-limit shape");

    expect(activePolicyForShape(readback, shape)?.operator_allowance).toBe(2);
    expect(
      activePolicyForShape(readback, { ...shape, shape_fingerprint: fingerprint("other") }),
    ).toBeUndefined();
  });

  test("shortens the machine compatibility token without exposing its full digest", () => {
    expect(shortFingerprint(fingerprint("1234567890abcdef"))).toBe("1234567890ab…");
    expect(shortFingerprint("")).toBe("—");
  });

  test("distinguishes active pinned work from a previous Release cohort", () => {
    const readback = executionLimitReadback();
    const active = readback.enforced.active_release[0];
    if (!active) throw new Error("expected an active execution-limit shape");
    const residual = { ...active, queued: 1, running: 0 };

    expect(residualMatchesActiveShape(residual, [active])).toBe(true);
    expect(
      residualMatchesActiveShape({ ...residual, shape_fingerprint: fingerprint("previous") }, [
        active,
      ]),
    ).toBe(false);
    expect(residualMatchesActiveShape({ ...residual, release_ceiling: 4 }, [active])).toBe(false);
  });
});

function fingerprint(digest: string): string {
  return `elfp:v1:sha256:${digest}`;
}

function executionLimitReadback(): ExecutionLimitPolicyReadback {
  const shapeFingerprint = fingerprint("active");
  return {
    workspace_id: "default",
    app_key: "echo",
    desired: {
      items: [
        {
          workspace_id: "default",
          app_key: "echo",
          scope: "app",
          policy_id: "app-concurrency",
          kind: "concurrency",
          shape_fingerprint: shapeFingerprint,
          operator_allowance: 2,
          revision: 1,
          operation_id: "op-1",
          status: "applied",
          updated_at: "2026-08-16T00:00:00Z",
        },
      ],
    },
    observed: {
      commit_sha: "abc123",
      items: [
        {
          workspace_id: "default",
          app_key: "echo",
          scope: "app",
          policy_id: "app-concurrency",
          kind: "concurrency",
          shape_fingerprint: shapeFingerprint,
          release_ceiling: 5,
        },
      ],
    },
    enforced: {
      active_release: [
        {
          workspace_id: "default",
          app_key: "echo",
          scope: "app",
          policy_id: "app-concurrency",
          kind: "concurrency",
          shape_fingerprint: shapeFingerprint,
          release_ceiling: 5,
          operator_allowance: 2,
          effective_limit: 2,
          policy_revision: 1,
          status: "operator_allowance",
          over_allowance_drain: false,
        },
      ],
      residual_cohorts: [],
    },
  };
}

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
