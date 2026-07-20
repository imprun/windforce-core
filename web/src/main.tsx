import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@fontsource-variable/ibm-plex-sans";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";
import { App } from "./App";
import { QueryProvider } from "./app/query-provider";
import { AppProvider } from "./lib/app-context";
import { RouterProvider } from "./lib/router";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root element");

createRoot(root).render(
  <StrictMode>
    <QueryProvider>
      <RouterProvider>
        <AppProvider>
          <App />
        </AppProvider>
      </RouterProvider>
    </QueryProvider>
  </StrictMode>,
);
