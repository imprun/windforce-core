// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { ConfirmDialog, ProbeNotice, SelectControl } from "./ui";

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

  test("requires an exact typed value when destructive confirmation requests it", () => {
    const onConfirm = vi.fn();

    render(
      <ConfirmDialog
        title="Delete workspace"
        description="This cannot be undone."
        confirmLabel="Delete workspace"
        confirmation={{ label: 'Type "Operations" to confirm.', expected: "Operations" }}
        onCancel={vi.fn()}
        onConfirm={onConfirm}
      />,
    );

    const confirmButton = screen.getByRole("button", { name: "Delete workspace" });
    const confirmationInput = screen.getByRole("textbox", {
      name: 'Type "Operations" to confirm.',
    });
    expect((confirmButton as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(confirmationInput, { target: { value: "operations" } });
    expect((confirmButton as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(confirmationInput, { target: { value: "Operations" } });
    expect((confirmButton as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(confirmButton);
    expect(onConfirm).toHaveBeenCalledOnce();
  });
});

describe("ProbeNotice", () => {
  test("renders a missing branch as an error instead of a successful probe", () => {
    const { container } = render(
      <ProbeNotice
        branch="missing"
        probe={{
          reachable: true,
          branch: "missing",
          branch_exists: false,
          branches: ["main"],
          code: "git_source_branch_not_found",
        }}
      />,
    );

    expect(container.querySelector(".inlineNotice.error")).not.toBeNull();
    expect(container.querySelector(".inlineNotice.ok")).toBeNull();
  });
});
