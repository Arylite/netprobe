import { describe, expect, it } from "vitest";

import { loadConfig, parseConfig } from "./config";

describe("parseConfig", () => {
  it("takes the address of the central and drops the trailing slash", () => {
    expect(parseConfig({ apiUrl: "https://central.example.com/" })).toEqual({ apiUrl: "https://central.example.com" });
    expect(parseConfig({ apiUrl: "http://127.0.0.1:8081" })).toEqual({ apiUrl: "http://127.0.0.1:8081" });
  });

  it("refuses anything else, and says what is expected", () => {
    for (const bad of [null, {}, { apiUrl: 42 }, { apiUrl: "" }, { apiUrl: "central.example.com" }, { apiUrl: "javascript:alert(1)" }, { apiUrl: "ftp://x" }]) {
      expect(() => parseConfig(bad)).toThrow(/apiUrl/);
    }
  });
});

describe("loadConfig", () => {
  it("reads config.json", async () => {
    const fetchImpl = (async () => new Response(JSON.stringify({ apiUrl: "https://c.example.com" }))) as typeof fetch;
    await expect(loadConfig(fetchImpl)).resolves.toEqual({ apiUrl: "https://c.example.com" });
  });

  it("explains a missing or broken file", async () => {
    await expect(loadConfig((async () => new Response("", { status: 404 })) as typeof fetch)).rejects.toThrow(/404/);
    await expect(loadConfig((async () => new Response("not json")) as typeof fetch)).rejects.toThrow(/apiUrl/);
    await expect(loadConfig((async () => Promise.reject(new Error("offline"))) as typeof fetch)).rejects.toThrow(/config.json/);
  });
});
