import { render } from "@testing-library/react";
import { createMemoryRouter } from "react-router";

import { App, newQueryClient, routes } from "../App";

export interface Call {
  method: string;
  path: string;
  body: unknown;
  headers: Headers;
}

export interface Answer {
  status?: number;
  body?: unknown;
}

type Handler = Answer | ((call: Call) => Answer);

/** A central that answers from a table of "METHOD /path" to what it says. */
export function mockCentral(table: Record<string, Handler>) {
  const calls: Call[] = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    const req = input instanceof Request ? input : new Request(input, init);
    const text = await req.clone().text();
    const call: Call = { method: req.method, path: new URL(req.url).pathname, body: text ? JSON.parse(text) : undefined, headers: req.headers };
    calls.push(call);
    const handler = table[`${call.method} ${call.path}`];
    if (!handler) return new Response(JSON.stringify({ error: `nothing mocked for ${call.method} ${call.path}` }), { status: 500 });
    const answer = typeof handler === "function" ? handler(call) : handler;
    const status = answer.status ?? 200;
    return new Response(status === 204 ? null : JSON.stringify(answer.body ?? {}), {
      status,
      headers: { "Content-Type": "application/json" },
    });
  };
  return { fetch: fetchImpl, calls, called: (key: string) => calls.filter((c) => `${c.method} ${c.path}` === key) };
}

export const admin = { username: "alice", role: "admin" } as const;
export const viewer = { username: "bob", role: "viewer" } as const;

/** Signs somebody in before the app starts, as a reload of the tab would find it. */
export function signedInAs(user: { username: string; role: string }) {
  sessionStorage.setItem(
    "netprobe.session",
    JSON.stringify({ token: "session-token", expiresAt: new Date(Date.now() + 3600_000).toISOString(), user }),
  );
}

export function renderApp(central: ReturnType<typeof mockCentral>, path = "/") {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const queryClient = newQueryClient();
  queryClient.setDefaultOptions({ queries: { retry: false, staleTime: 0, refetchInterval: false } });
  render(<App config={{ apiUrl: "http://central.test" }} fetch={central.fetch} router={router} queryClient={queryClient} testing />);
  return { router };
}

/** The answers of the read endpoints, empty, for a test that is about something else. */
export const quietCentral: Record<string, Answer> = {
  "GET /api/v1/setup": { body: { required: false } },
  "GET /api/v1/status": { body: { window_seconds: 86400, checks: [] } },
  "GET /api/v1/incidents": { body: { incidents: [] } },
  "GET /api/v1/edges": { body: { edges: [] } },
  "GET /api/v1/checks": { body: { checks: [] } },
  "GET /api/v1/channels": { body: { channels: [] } },
  "GET /api/v1/users": { body: { users: [] } },
};
