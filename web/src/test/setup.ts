import "@testing-library/jest-dom/vitest";
import { notifications } from "@mantine/notifications";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

afterEach(() => {
  cleanup();
  // The store of notifications is global, and shows five at a time: what one test
  // leaves would queue the next one's.
  notifications.clean();
  sessionStorage.clear();
  localStorage.clear();
});

// jsdom has none of what Mantine measures with.
Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }),
});

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal("ResizeObserver", ResizeObserverStub);
window.HTMLElement.prototype.scrollIntoView = vi.fn();
