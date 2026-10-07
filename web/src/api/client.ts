import createClient from "openapi-fetch";

import type { components, paths } from "./schema";

export type Schemas = components["schemas"];
export type Edge = Schemas["Edge"];
export type Check = Schemas["Check"];
export type Result = Schemas["Result"];
export type User = Schemas["User"];
export type Session = Schemas["Session"];
export type CheckStatus = Schemas["CheckStatus"];
export type EdgeStatus = Schemas["EdgeStatus"];
export type Incident = Schemas["Incident"];
export type Channel = Schemas["Channel"];

export type Api = ReturnType<typeof createApi>;

export interface ApiOptions {
  baseUrl: string;
  /** The session token, or null when nobody is signed in. */
  getToken: () => string | null;
  /** Called when the central refuses a token that was sent: the session is over. */
  onUnauthorized: () => void;
  fetch?: typeof fetch;
}

/** The typed client of the UI API; it sends the session token with every call. */
export function createApi(opts: ApiOptions) {
  const client = createClient<paths>({ baseUrl: opts.baseUrl, fetch: opts.fetch });
  client.use({
    onRequest({ request }) {
      const token = opts.getToken();
      if (token) request.headers.set("Authorization", `Bearer ${token}`);
      return request;
    },
    onResponse({ response }) {
      // Without a token the 401 is a refused login, not an ended session.
      if (response.status === 401 && opts.getToken()) opts.onUnauthorized();
    },
  });
  return client;
}

/** What went wrong in a call, in words for the person at the screen. */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

type Settled<T> = { data?: T; error?: unknown; response: Response };

function messageOf(error: unknown, status: number): string {
  const text = (error as { error?: unknown } | null)?.error;
  if (typeof text === "string" && text !== "") return text.charAt(0).toUpperCase() + text.slice(1);
  if (status === 503) return "The central is unavailable, try again in a moment.";
  return `Unexpected answer from the central (${status}).`;
}

async function settle<T extends { response: Response; error?: unknown }>(call: Promise<T>): Promise<T> {
  let result: T;
  try {
    result = await call;
  } catch {
    throw new ApiError(0, "The central cannot be reached.");
  }
  if (!result.response.ok) throw new ApiError(result.response.status, messageOf(result.error, result.response.status));
  return result;
}

/** Resolves with the body of a successful call and throws an ApiError otherwise. */
export async function unwrap<T>(call: Promise<Settled<T>>): Promise<T> {
  return (await settle(call)).data as T;
}

/** The same for a call that answers no body. */
export async function unwrapEmpty(call: Promise<{ response: Response; error?: unknown }>): Promise<void> {
  await settle(call);
}

/** The message of whatever a failed call threw. */
export function errorText(err: unknown): string {
  if (typeof err === "string") return err;
  return err instanceof Error ? err.message : "Something went wrong.";
}
