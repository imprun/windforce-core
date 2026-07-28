// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { SelectControl } from "./ui";

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
    expect(screen.getByRole("combobox", { name: "Output delivery" }).textContent).toContain(
      "Poll Invocation API",
    );
  });
});
