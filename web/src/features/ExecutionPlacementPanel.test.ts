import { describe, expect, test } from "vitest";
import type { WorkerView } from "../lib/api";
import { actionReleaseLabels, countMatchingWorkers } from "./ExecutionPlacementPanel";

function worker(
  id: string,
  { tags = [], labels = [], live = true }: Partial<Pick<WorkerView, "tags" | "labels" | "live">>,
): WorkerView {
  return {
    id,
    tags,
    labels,
    live,
    slots: 1,
    started_at: "2026-08-10T00:00:00Z",
    last_heartbeat_at: "2026-08-10T00:00:00Z",
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
});
