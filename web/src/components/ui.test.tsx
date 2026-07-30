// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { ConfirmDialog, SelectControl } from "./ui";

describe("SelectControl", () => {
  test("uses the shared accessible popup primitive instead of a native select", () => {
    const { container } = render(
      <SelectControl
        value="poll"
        onChange={vi.fn()}
        ariaLabel="Output delivery"
        options={[
          { value: "poll", label: "Poll Invocation API" },
          { value: "callback", label: "HTTP callback" },
        ]}
      />,
    );

    expect(container.querySelector("select")).toBeNull();
    const control = screen.getByRole("combobox", { name: "Output delivery" });
    expect(control.querySelector(".selectValue")).not.toBeNull();
    expect(control.textContent).toContain("Poll Invocation API");
  });

  test("renders destructive confirmation inside the shared accessible dialog", () => {
    const onCancel = vi.fn();
    const onConfirm = vi.fn();

    render(
      <ConfirmDialog
        title="Revoke client token"
        description="Invocation API calls will stop immediately."
        confirmLabel="Revoke token"
        onCancel={onCancel}
        onConfirm={onConfirm}
      />,
    );

    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Revoke client token" })).toBeTruthy();
    expect(screen.getByText("Invocation API calls will stop immediately.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Revoke token" }).className).toContain(
      "danger filled",
    );
  });
});
