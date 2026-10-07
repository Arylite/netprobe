import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";

const policy = [
  "default-src 'none'",
  "script-src 'self'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data:",
  "font-src 'self'",
  "connect-src 'self' http: https:",
  "base-uri 'none'",
  "form-action 'none'",
].join("; ");

const contentSecurityPolicy = (): Plugin => ({
  name: "netprobe-csp",
  apply: "build",
  transformIndexHtml: () => [{ tag: "meta", attrs: { "http-equiv": "Content-Security-Policy", content: policy }, injectTo: "head-prepend" }],
});

export default defineConfig({
  base: "./",
  plugins: [react(), contentSecurityPolicy()],
  // The component library is most of the weight; an administration page that loads once is not worth splitting.
  build: { sourcemap: false, target: "es2022", chunkSizeWarningLimit: 900 },
  test: { environment: "jsdom", setupFiles: ["./src/test/setup.ts"], css: false },
});
