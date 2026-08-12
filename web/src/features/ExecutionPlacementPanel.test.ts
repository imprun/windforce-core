import { describe, expect, test } from "vitest";
import type { ExecutionProfileView, WorkerView } from "../lib/api";
import { actionReleaseLabels, countMatchingWorkers } from "./ExecutionPlacementPanel";

function worker(
  id: string,
  {
    tags = [],
    labels = [],
    execution_profiles = [],
    live = true,
  }: Partial<Pick<WorkerView, "tags" | "labels" | "execution_profiles" | "live">>,
): WorkerView {
  return {
    id,
    tags,
    labels,
    execution_profiles,
    live,
    slots: 1,
    started_at: "2026-08-10T00:00:00Z",
    last_heartbeat_at: "2026-08-10T00:00:00Z",
  };
}

function executionProfile(key: string): ExecutionProfileView {
  return {
    version: "execution-profile-v1",
    key,
    os: "linux",
    arch: "amd64",
    runtime: "bun",
    runtimeAbi: "bun-1",
    libc: "glibc",
  };
}

describe("execution placement", () => {
  test("shows the Action release-label default as the App and Action runsOn union", () => {
    expect(actionReleaseLabels(["linux", "browser"], ["gpu", "browser"])).toEqual([
      "browser",
      "gpu",
      "linux",
    ]);
  });

  test("matches only live workers satisfying the worker tag and every required label", () => {
    const workers = [
      worker("exact", { tags: ["browser"], labels: ["gpu", "linux"] }),
      worker("wildcard-tag", { labels: ["gpu", "linux"] }),
      worker("missing-label", { tags: ["browser"], labels: ["gpu"] }),
      worker("wrong-tag", { tags: ["python"], labels: ["gpu", "linux"] }),
      worker("stale", { tags: ["browser"], labels: ["gpu", "linux"], live: false }),
    ];

    expect(countMatchingWorkers(workers, "browser", ["gpu", "linux"])).toBe(2);
    expect(countMatchingWorkers(workers, "browser", [])).toBe(3);
  });

  test("matches reserved execution-profile labels advertised as structured profiles", () => {
    const requiredKey = "6a0c523fca745c72e67022c7c48dca7391398c9cb1d353f24a353141e3c92316";
    const wrongKey = "7bc88fb217f83f6759187cd62f1083ed4bc56783e00605cc27b300de0ae2690e";
    const workers = [
      worker("matching", { execution_profiles: [executionProfile(requiredKey)] }),
      worker("wrong-profile", { execution_profiles: [executionProfile(wrongKey)] }),
      worker("stale", { execution_profiles: [executionProfile(requiredKey)], live: false }),
    ];

    expect(
      countMatchingWorkers(workers, "default", [
        `sys/execution-profile-${requiredKey.slice(0, 24)}`,
      ]),
    ).toBe(1);
  });
});
