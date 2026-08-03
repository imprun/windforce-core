// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, test, vi } from "vitest";
import { WebhookEventPicker } from "./WebhookEventPicker";

describe("WebhookEventPicker", () => {
  afterEach(cleanup);

  test("adds a HumanTask lifecycle event without changing the existing release defaults", async () => {
    const onChange = vi.fn();
    render(
      <WebhookEventPicker
        selected={["windforce.release.published", "windforce.release.rolled_back"]}
        onChange={onChange}
      />,
    );

    await userEvent.click(screen.getByRole("checkbox", { name: /Human task created/i }));

    expect(onChange).toHaveBeenCalledWith([
      "windforce.human_task.created",
      "windforce.release.published",
      "windforce.release.rolled_back",
    ]);
  });
});
