import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type AnchorHTMLAttributes,
  type MouseEvent,
  type ReactNode,
} from "react";

// BASE is "/ui/" in both dev and production builds. The fallback keeps the
// module importable outside Vite (e.g. bun test).
const BASE = (import.meta.env?.BASE_URL ?? "/").replace(/\/$/, "");

type RouterState = {
  path: string;
  navigate: (to: string, options?: { replace?: boolean }) => void;
};

const RouterContext = createContext<RouterState>({
  path: "/",
  navigate: () => {},
});

function currentPath(): string {
  let path = window.location.pathname;
  if (path.startsWith(BASE)) path = path.slice(BASE.length);
  if (!path.startsWith("/")) path = `/${path}`;
  return path;
}

export function RouterProvider({ children }: { children: ReactNode }) {
  const [path, setPath] = useState(currentPath);

  useEffect(() => {
    const sync = () => setPath(currentPath());
    window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, []);

  const navigate = useCallback((to: string, options?: { replace?: boolean }) => {
    const url = href(to);
    if (options?.replace) {
      window.history.replaceState(null, "", url);
    } else {
      window.history.pushState(null, "", url);
    }
    setPath(currentPath());
    window.scrollTo(0, 0);
  }, []);

  return <RouterContext.Provider value={{ path, navigate }}>{children}</RouterContext.Provider>;
}

export function useRouter(): RouterState {
  return useContext(RouterContext);
}

export function href(to: string): string {
  return `${BASE}${to.startsWith("/") ? to : `/${to}`}`;
}

type LinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & { to: string };

export function Link({ to, onClick, children, ...rest }: LinkProps) {
  const { navigate } = useRouter();
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    if (event.defaultPrevented) return;
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate(to);
  };
  return (
    <a href={href(to)} onClick={handleClick} {...rest}>
      {children}
    </a>
  );
}

function safeDecode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

// matchRoute("/apps/:id/:tab?", "/apps/3/releases") -> { id: "3", tab: "releases" }
export function matchRoute(pattern: string, path: string): Record<string, string> | null {
  const patternParts = pattern.split("/").filter(Boolean);
  const pathParts = path.split("/").filter(Boolean).map(safeDecode);
  const params: Record<string, string> = {};
  let i = 0;
  for (const part of patternParts) {
    const optional = part.endsWith("?");
    const name = part.replace(/[:?]/g, "");
    if (part.startsWith(":")) {
      if (i >= pathParts.length) {
        if (optional) continue;
        return null;
      }
      params[name] = pathParts[i];
      i += 1;
    } else {
      if (pathParts[i] !== part) return null;
      i += 1;
    }
  }
  if (i !== pathParts.length) return null;
  return params;
}
