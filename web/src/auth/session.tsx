import { useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { createApi, unwrap, unwrapEmpty, type Api, type Session, type User } from "../api/client";
import type { Config } from "../config";

const STORAGE_KEY = "netprobe.session";

interface Stored {
  token: string;
  expiresAt: string;
  user: User;
}

// The token lives in sessionStorage: it goes away with the tab, and nothing
// else on the machine, nor another site, can read it.
function read(now: number): Stored | null {
  try {
    const raw = JSON.parse(sessionStorage.getItem(STORAGE_KEY) ?? "null") as Stored | null;
    if (raw && typeof raw.token === "string" && raw.user && Date.parse(raw.expiresAt) > now) return raw;
  } catch {
    // Unreadable storage or a corrupt entry: nobody is signed in.
  }
  return null;
}

function write(stored: Stored | null) {
  try {
    if (stored) sessionStorage.setItem(STORAGE_KEY, JSON.stringify(stored));
    else sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // Storage may be blocked: the session then lasts until the page is reloaded.
  }
}

export interface SessionContext {
  api: Api;
  user: User | null;
  /** Why the person is signed out, when it was not their doing. */
  notice: string | null;
  login: (username: string, password: string) => Promise<void>;
  /** Creates the first administrator of a central that has no account, and signs them in. */
  setup: (code: string, username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  /** Forgets the session locally, for when the central already ended it. */
  endSession: (notice?: string) => void;
}

const Context = createContext<SessionContext | null>(null);

export function SessionProvider({ config, fetch: fetchImpl, children }: { config: Config; fetch?: typeof fetch; children: ReactNode }) {
  const queryClient = useQueryClient();
  const [stored, setStored] = useState<Stored | null>(() => read(Date.now()));
  const [notice, setNotice] = useState<string | null>(null);
  const tokenRef = useRef<string | null>(stored?.token ?? null);

  const endSession = useCallback(
    (message?: string) => {
      tokenRef.current = null;
      write(null);
      setStored(null);
      setNotice(message ?? null);
      queryClient.clear();
    },
    [queryClient],
  );

  const api = useMemo(
    () =>
      createApi({
        baseUrl: config.apiUrl,
        fetch: fetchImpl,
        getToken: () => tokenRef.current,
        onUnauthorized: () => endSession("Your session has ended. Sign in again."),
      }),
    [config.apiUrl, fetchImpl, endSession],
  );

  // A session ends at the time the central said, whether or not it is asked.
  const expiresAt = stored?.expiresAt;
  useEffect(() => {
    if (!expiresAt) return;
    const wait = Math.min(Date.parse(expiresAt) - Date.now(), 2 ** 31 - 1);
    const timer = setTimeout(() => endSession("Your session has expired. Sign in again."), Math.max(wait, 0));
    return () => clearTimeout(timer);
  }, [expiresAt, endSession]);

  const start = useCallback((session: Session) => {
    const next = { token: session.token, expiresAt: session.expires_at, user: session.user };
    tokenRef.current = next.token;
    write(next);
    setNotice(null);
    setStored(next);
  }, []);

  const login = useCallback(
    async (username: string, password: string) => start(await unwrap(api.POST("/api/v1/login", { body: { username, password } }))),
    [api, start],
  );

  const setup = useCallback(
    async (code: string, username: string, password: string) =>
      start(await unwrap(api.POST("/api/v1/setup", { body: { code, username, password } }))),
    [api, start],
  );

  const logout = useCallback(async () => {
    try {
      await unwrapEmpty(api.POST("/api/v1/logout"));
    } catch {
      // The session ends here either way; the central drops it when it expires.
    }
    endSession();
  }, [api, endSession]);

  const value = useMemo<SessionContext>(
    () => ({ api, user: stored?.user ?? null, notice, login, setup, logout, endSession }),
    [api, stored, notice, login, setup, logout, endSession],
  );
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

export function useSession(): SessionContext {
  const value = useContext(Context);
  if (!value) throw new Error("useSession needs a SessionProvider");
  return value;
}

/** The signed-in user; only used below the route that requires a session. */
export function useUser(): User {
  const { user } = useSession();
  if (!user) throw new Error("no user is signed in");
  return user;
}
