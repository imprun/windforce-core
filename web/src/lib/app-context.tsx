import { useQueryClient } from "@tanstack/react-query";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { errorMessage, loadSettings, type Settings, saveSettings, WindforceApi } from "./api";
import { loadRuntimeConfig, type RuntimeConfig } from "./runtime-config";

export type Toast = {
  id: number;
  tone: "ok" | "error" | "info";
  text: string;
};

type AppContextValue = {
  settings: Settings;
  updateSettings: (next: Settings) => void;
  clearLocalCredentials: () => void;
  localAccessOpen: boolean;
  requestLocalAccess: () => void;
  dismissLocalAccess: () => void;
  api: WindforceApi;
  runtimeConfig: RuntimeConfig | null;
  toasts: Toast[];
  notify: (tone: Toast["tone"], text: string) => void;
  dismissToast: (id: number) => void;
};

const AppContext = createContext<AppContextValue | null>(null);

export function AppProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [settings, setSettings] = useState<Settings>(loadSettings);
  const [runtimeConfig, setRuntimeConfig] = useState<RuntimeConfig | null>(null);
  const [localAccessOpen, setLocalAccessOpen] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextToastID = useRef(1);

  useEffect(() => {
    saveSettings(settings);
  }, [settings]);

  useEffect(() => {
    let active = true;
    void loadRuntimeConfig()
      .then((config) => {
        if (active) setRuntimeConfig(config);
      })
      .catch(() => {
        if (active) {
          setRuntimeConfig({ hostConsole: null, hostAccount: null, authMode: "disabled" });
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const dismissToast = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id));
  }, []);

  const notify = useCallback(
    (tone: Toast["tone"], text: string) => {
      const id = nextToastID.current;
      nextToastID.current += 1;
      setToasts((current) => [...current.slice(-2), { id, tone, text }]);
      if (tone !== "error") {
        window.setTimeout(() => dismissToast(id), 3200);
      }
    },
    [dismissToast],
  );

  const clearLocalCredentials = useCallback(() => {
    queryClient.clear();
    setToasts([]);
    setSettings((current) => ({ ...current, actor: "", token: "" }));
  }, [queryClient]);

  const requestLocalAccess = useCallback(() => setLocalAccessOpen(true), []);
  const dismissLocalAccess = useCallback(() => setLocalAccessOpen(false), []);

  useEffect(() => {
    if (runtimeConfig?.authMode === "browser_token" && !settings.token) {
      setLocalAccessOpen(true);
    } else if (runtimeConfig && runtimeConfig.authMode !== "browser_token") {
      setLocalAccessOpen(false);
    }
  }, [runtimeConfig, settings.token]);

  const api = useMemo(() => {
    const usesBrowserCredentials =
      runtimeConfig === null || runtimeConfig.authMode === "browser_token";
    return new WindforceApi(
      usesBrowserCredentials ? settings : { ...settings, token: "", actor: "" },
      runtimeConfig?.authMode === "browser_token" ? requestLocalAccess : undefined,
    );
  }, [settings, runtimeConfig, requestLocalAccess]);
  const value = useMemo(
    () => ({
      settings,
      updateSettings: setSettings,
      clearLocalCredentials,
      localAccessOpen,
      requestLocalAccess,
      dismissLocalAccess,
      api,
      runtimeConfig,
      toasts,
      notify,
      dismissToast,
    }),
    [
      settings,
      clearLocalCredentials,
      localAccessOpen,
      requestLocalAccess,
      dismissLocalAccess,
      api,
      runtimeConfig,
      toasts,
      notify,
      dismissToast,
    ],
  );

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>;
}

export function useApp(): AppContextValue {
  const value = useContext(AppContext);
  if (!value) throw new Error("useApp must be used inside AppProvider");
  return value;
}

export type AsyncState<T> = {
  data: T | null;
  loading: boolean;
  error: string | null;
  reload: () => void;
};

export function useAsync<T>(load: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: callers define the load contract.
  useEffect(() => {
    void tick;
    let canceled = false;
    setLoading(true);
    load()
      .then((result) => {
        if (canceled) return;
        setData(result);
        setError(null);
      })
      .catch((cause: unknown) => {
        if (canceled) return;
        setError(errorMessage(cause));
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, [...deps, tick]);

  const reload = useCallback(() => setTick((current) => current + 1), []);
  return { data, loading, error, reload };
}
