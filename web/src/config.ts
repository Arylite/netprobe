export interface Config {
  /** Address of the netprobe UI API, without a trailing slash. */
  apiUrl: string;
}

/** Reads config.json, next to index.html: it says which central to talk to. */
export async function loadConfig(fetchImpl: typeof fetch = fetch): Promise<Config> {
  let res: Response;
  try {
    res = await fetchImpl(`${import.meta.env.BASE_URL}config.json`, { cache: "no-store" });
  } catch {
    throw new Error("config.json cannot be loaded.");
  }
  if (!res.ok) throw new Error(`config.json cannot be loaded (${res.status}).`);
  return parseConfig(await res.json().catch(() => null));
}

export function parseConfig(raw: unknown): Config {
  const apiUrl = (raw as { apiUrl?: unknown } | null)?.apiUrl;
  if (typeof apiUrl !== "string" || !/^https?:\/\/[^/\s]+/.test(apiUrl)) {
    throw new Error('config.json must hold {"apiUrl": "https://central.example.com"}.');
  }
  return { apiUrl: apiUrl.replace(/\/+$/, "") };
}
