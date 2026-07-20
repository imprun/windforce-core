import { create } from "zustand";

export type ThemePreference = "system" | "light" | "dark";

function applyTheme(preference: ThemePreference) {
  if (typeof document === "undefined") return;
  if (preference === "system") {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.dataset.theme = preference;
  }
}

const initialPreference = (): ThemePreference => {
  const stored = globalThis.localStorage?.getItem("wf.theme");
  return stored === "light" || stored === "dark" ? stored : "system";
};

type ThemeState = {
  preference: ThemePreference;
  cycle: () => void;
};

export const useThemeStore = create<ThemeState>((set, get) => {
  const preference = initialPreference();
  applyTheme(preference);
  return {
    preference,
    cycle: () => {
      const current = get().preference;
      const next = current === "system" ? "light" : current === "light" ? "dark" : "system";
      globalThis.localStorage?.setItem("wf.theme", next);
      applyTheme(next);
      set({ preference: next });
    },
  };
});
