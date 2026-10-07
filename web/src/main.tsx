import "@mantine/core/styles.css";

import { Alert, Center, MantineProvider } from "@mantine/core";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { loadConfig } from "./config";
import { theme } from "./theme";

const container = document.getElementById("root");
if (!container) throw new Error("index.html has no #root");
const root = createRoot(container);

loadConfig()
  .then((config) =>
    root.render(
      <StrictMode>
        <App config={config} />
      </StrictMode>,
    ),
  )
  .catch((err: unknown) =>
    root.render(
      <MantineProvider theme={theme} defaultColorScheme="light">
        <Center mih="100vh" p="md">
          <Alert color="red" title="netprobe cannot start" maw={520}>
            {err instanceof Error ? err.message : "The configuration cannot be read."}
          </Alert>
        </Center>
      </MantineProvider>,
    ),
  );
