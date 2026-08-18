import { describe, expect, test } from "vitest";
import type { JobResultResponse } from "../lib/api";
import {
  isHumanTaskDeadlineResult,
  readTerminalJobResult,
  shortOpaqueDigest,
} from "./JobLogInspector";

describe("shortOpaqueDigest", () => {
  test("keeps the algorithm and enough digest material to compare pins", () => {
    expect(shortOpaqueDigest("hmac-sha256:0123456789abcdef")).toBe("hmac-sha256:0123456789ab");
    expect(shortOpaqueDigest("sha256:fedcba9876543210")).toBe("sha256:fedcba987654");
  });

  test("shortens values without an algorithm prefix", () => {
    expect(shortOpaqueDigest("0123456789abcdef", 8)).toBe("01234567");
  });
});

describe("isHumanTaskDeadlineResult", () => {
  test("separates the stable HumanTask deadline code from ordinary Action failures", () => {
    expect(
      isHumanTaskDeadlineResult({
        status: "failure",
        result: { name: "Error", message: "HumanTask is expired", code: "human_task_deadline" },
      }),
    ).toBe(true);
    expect(
      isHumanTaskDeadlineResult({
        status: "failure",
        result: { name: "Error", message: "ordinary action failure" },
      }),
    ).toBe(false);
    expect(
      isHumanTaskDeadlineResult({
        status: "success",
        result: { code: "human_task_deadline" },
      }),
    ).toBe(false);
  });
});

describe("readTerminalJobResult", () => {
  test("retries a pending projection without turning lookup failure into a log-stream failure", async () => {
    const responses: JobResultResponse[] = [
      { status: "pending" },
      { status: "pending" },
      { status: "failure", result: { code: "human_task_deadline" } },
    ];
    let calls = 0;
    const result = await readTerminalJobResult(
      async () => responses[calls++]!,
      async () => undefined,
    );
    expect(calls).toBe(3);
    expect(result).toEqual(responses[2]);

    calls = 0;
    await expect(
      readTerminalJobResult(
        async () => {
          calls += 1;
          return { status: "pending" };
        },
        async () => undefined,
      ),
    ).resolves.toBeNull();
    expect(calls).toBe(3);

    await expect(
      readTerminalJobResult(
        async () => {
          throw new Error("result unavailable");
        },
        async () => undefined,
      ),
    ).resolves.toBeNull();
  });
});
