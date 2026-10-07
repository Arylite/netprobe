import { useQuery } from "@tanstack/react-query";

import { useSession } from "../auth/session";
import { unwrap } from "./client";

// What is on screen is refreshed now and then, so that an incident that opens
// while the page is up shows without a reload.
const REFRESH_MS = 15_000;

/** Whether the central still waits for its first account. Asked before anyone signs in. */
export function useSetupStatus() {
  const { api } = useSession();
  return useQuery({
    queryKey: ["setup"],
    queryFn: async () => (await unwrap(api.GET("/api/v1/setup"))).required,
    staleTime: 0,
    retry: false,
  });
}

export function useStatus() {
  const { api } = useSession();
  return useQuery({
    queryKey: ["status"],
    queryFn: () => unwrap(api.GET("/api/v1/status")),
    refetchInterval: REFRESH_MS,
  });
}

export function useEdges() {
  const { api } = useSession();
  return useQuery({
    queryKey: ["edges"],
    queryFn: async () => (await unwrap(api.GET("/api/v1/edges"))).edges,
    refetchInterval: REFRESH_MS,
  });
}

export function useChecks() {
  const { api } = useSession();
  return useQuery({
    queryKey: ["checks"],
    queryFn: async () => (await unwrap(api.GET("/api/v1/checks"))).checks,
  });
}

export function useIncidents(state: "all" | "open") {
  const { api } = useSession();
  return useQuery({
    queryKey: ["incidents", state],
    queryFn: async () => (await unwrap(api.GET("/api/v1/incidents", { params: { query: { state, limit: 200 } } }))).incidents,
    refetchInterval: REFRESH_MS,
  });
}

export function useResults(checkId: string, limit: number) {
  const { api } = useSession();
  return useQuery({
    queryKey: ["results", checkId, limit],
    queryFn: async () => (await unwrap(api.GET("/api/v1/checks/{id}/results", { params: { path: { id: checkId }, query: { limit } } }))).results,
    refetchInterval: REFRESH_MS,
  });
}

export function useChannels(enabled = true) {
  const { api } = useSession();
  return useQuery({
    enabled,
    queryKey: ["channels"],
    queryFn: async () => (await unwrap(api.GET("/api/v1/channels"))).channels,
  });
}

export function useAudit(limit: number) {
  const { api } = useSession();
  return useQuery({
    queryKey: ["audit", limit],
    queryFn: async () => (await unwrap(api.GET("/api/v1/audit", { params: { query: { limit } } }))).events,
    refetchInterval: REFRESH_MS,
  });
}

export function useUsers() {
  const { api } = useSession();
  return useQuery({
    queryKey: ["users"],
    queryFn: async () => (await unwrap(api.GET("/api/v1/users"))).users,
  });
}
