// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { AppProvider, useApp } from "./app-context";

function SessionProbe() {
  const { settings, clearLocalCredentials } = useApp();
  return (
    <div>
      <span data-testid="session">
        {settings.workspace}:{settings.actor}:{settings.token || "signed-out"}
      </span>
      <button type="button" onClick={clearLocalCredentials}>
        Clear local credentials
      </button>
    </div>
  );
}

function UnauthorizedProbe() {
  const { api, localAccessOpen, runtimeConfig } = useApp();

  useEffect(() => {
    if (runtimeConfig?.authMode === "browser_token") {
      void api.clients().catch(() => undefined);
    }
  }, [api, runtimeConfig]);

  return (
    <span data-testid="local-access-prompt">
      {runtimeConfig?.authMode || "loading"}:{localAccessOpen ? "open" : "closed"}
    </span>
  );
}

describe("AppProvider local credentials", () => {
  afterEach(() => vi.unstubAllGlobals());

  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("wf.workspace", "gale");
    localStorage.setItem("wf.actor", "operator");
    localStorage.setItem("wf.token", "workspace-secret");
  });

  test("removes browser identity while preserving workspace context", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    queryClient.setQueryData(["private"], { value: "cached" });

    render(
      <QueryClientProvider client={queryClient}>
        <AppProvider>
          <SessionProbe />
        </AppProvider>
      </QueryClientProvider>,
    );

    expect(screen.getByTestId("session").textContent).toBe("gale:operator:workspace-secret");

    await user.click(screen.getByRole("button", { name: "Clear local credentials" }));

    await waitFor(() => {
      expect(screen.getByTestId("session").textContent).toBe("gale::signed-out");
      expect(localStorage.getItem("wf.token")).toBe("");
      expect(localStorage.getItem("wf.actor")).toBe("");
    });
    expect(localStorage.getItem("wf.workspace")).toBe("gale");
    expect(queryClient.getQueryData(["private"])).toBeUndefined();
  });

  test("requests local access again after a browser-token request is unauthorized", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input).includes("/ui/config.json")) {
          return {
            ok: true,
            json: async () => ({ auth_mode: "browser_token" }),
          };
        }
        return {
          ok: false,
          status: 401,
          statusText: "Unauthorized",
          text: async () => JSON.stringify({ error: "unauthorized" }),
        };
      }),
    );
    const queryClient = new QueryClient();

    render(
      <QueryClientProvider client={queryClient}>
        <AppProvider>
          <UnauthorizedProbe />
        </AppProvider>
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("local-access-prompt").textContent).toBe("browser_token:open");
    });
  });
});
