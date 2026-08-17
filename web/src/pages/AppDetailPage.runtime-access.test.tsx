/** @vitest-environment jsdom */

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";
import type { ActionView } from "../lib/api";
import { RuntimeAccessSummary } from "./AppDetailPage";

describe("RuntimeAccessSummary", () => {
  afterEach(cleanup);

  test("renders legacy paths and explicit scoped targets without passing objects to React", () => {
    const action: ActionView = {
      id: "action-1",
      workspace_id: "default",
      app_key: "orders",
      action_key: "sync",
      updated_at: "2026-08-17T00:00:00Z",
      runtime_access: {
        variables: ["legacy/token", { scope: "app", path: "credentials/token" }],
        resources: [{ scope: "workspace", path: "database/main" }],
      },
    };

    render(<RuntimeAccessSummary action={action} />);

    expect(screen.getByText("$var:legacy/token")).toBeTruthy();
    expect(screen.getByText("$var@app:credentials/token")).toBeTruthy();
    expect(screen.getByText("$res@workspace:database/main")).toBeTruthy();
  });
});
