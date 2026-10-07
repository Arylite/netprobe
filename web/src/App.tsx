import "@mantine/core/styles.css";
import "@mantine/notifications/styles.css";

import { MantineProvider } from "@mantine/core";
import { ModalsProvider } from "@mantine/modals";
import { Notifications } from "@mantine/notifications";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { RouterProvider, createHashRouter, type RouteObject } from "react-router";

import { ApiError } from "./api/client";
import { SessionProvider } from "./auth/session";
import { AdminOnly, Layout, NotFound, RequireSession } from "./components/Layout";
import type { Config } from "./config";
import { Account } from "./pages/Account";
import { Audit } from "./pages/Audit";
import { Channels } from "./pages/Channels";
import { CheckResults } from "./pages/CheckResults";
import { Checks } from "./pages/Checks";
import { Edges } from "./pages/Edges";
import { Incidents } from "./pages/Incidents";
import { Login } from "./pages/Login";
import { Overview } from "./pages/Overview";
import { Setup } from "./pages/Setup";
import { Users } from "./pages/Users";
import { theme } from "./theme";

export const routes: RouteObject[] = [
  { path: "/login", element: <Login /> },
  { path: "/setup", element: <Setup /> },
  {
    element: <RequireSession />,
    children: [
      {
        element: <Layout />,
        children: [
          { index: true, element: <Overview /> },
          { path: "incidents", element: <Incidents /> },
          { path: "edges", element: <Edges /> },
          { path: "checks", element: <Checks /> },
          { path: "checks/:id", element: <CheckResults /> },
          {
            path: "channels",
            element: (
              <AdminOnly>
                <Channels />
              </AdminOnly>
            ),
          },
          {
            path: "users",
            element: (
              <AdminOnly>
                <Users />
              </AdminOnly>
            ),
          },
          {
            path: "audit",
            element: (
              <AdminOnly>
                <Audit />
              </AdminOnly>
            ),
          },
          { path: "account", element: <Account /> },
          { path: "*", element: <NotFound /> },
        ],
      },
    ],
  },
  { path: "*", element: <RequireSession /> },
];

/** A refused request is not retried; a failure of the network or the central is, twice. */
export function newQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 5_000,
        retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
      },
    },
  });
}

interface AppProps {
  config: Config;
  /** For tests: the network, and a router that does not need a browser address bar. */
  fetch?: typeof fetch;
  router?: ReturnType<typeof createHashRouter>;
  queryClient?: QueryClient;
  /** For tests: no transitions and no portals timing, which jsdom cannot honour. */
  testing?: boolean;
}

export function App({ config, fetch: fetchImpl, router, queryClient, testing }: AppProps) {
  const [client] = useState(() => queryClient ?? newQueryClient());
  // A hash router: the build works from any static host, at any path, without a rewrite rule.
  const [theRouter] = useState(() => router ?? createHashRouter(routes));
  return (
    <MantineProvider theme={theme} defaultColorScheme="light" env={testing ? "test" : "default"}>
      <ModalsProvider>
        <Notifications position="top-right" />
        <QueryClientProvider client={client}>
          <SessionProvider config={config} fetch={fetchImpl}>
            <RouterProvider router={theRouter} />
          </SessionProvider>
        </QueryClientProvider>
      </ModalsProvider>
    </MantineProvider>
  );
}
