import { Button, CloseButton, Group, Paper, Stack, Text, ThemeIcon, Title } from "@mantine/core";
import { IconArrowRight, IconCheck, IconRocket } from "@tabler/icons-react";
import { useState } from "react";
import { Link } from "react-router";

import { useChannels, useChecks, useEdges } from "../api/queries";
import { useUser } from "../auth/session";

const KEY = "netprobe.gettingStarted.hidden";

function hidden(): boolean {
  try {
    return localStorage.getItem(KEY) === "1";
  } catch {
    return false;
  }
}

interface Step {
  title: string;
  text: string;
  to: string;
  action: string;
  done: boolean;
}

/** The first three steps of an empty central; it hides when they are done, or on request. */
export function GettingStarted() {
  const user = useUser();
  const admin = user.role === "admin";
  const edges = useEdges();
  const checks = useChecks();
  const channels = useChannels(admin);
  const [closed, setClosed] = useState(hidden);

  if (!admin || closed || edges.data === undefined || checks.data === undefined || channels.data === undefined) return null;

  const steps: Step[] = [
    {
      title: "Register an edge",
      text: "An edge is a machine that runs your checks. Add one and give it its token.",
      to: "/edges",
      action: "Add an edge",
      done: edges.data.some((e) => !e.revoked_at),
    },
    {
      title: "Add a check",
      text: "Say what to measure: a TCP port or an HTTP address. Every edge runs it.",
      to: "/checks",
      action: "Add a check",
      done: checks.data.length > 0,
    },
    {
      title: "Say who to tell",
      text: "Add a webhook channel, to hear of an incident when it opens and when it ends.",
      to: "/channels",
      action: "Add a channel",
      done: channels.data.length > 0,
    },
  ];
  if (steps.every((s) => s.done)) return null;
  const next = steps.findIndex((s) => !s.done);

  return (
    <Paper p="lg" aria-labelledby="getting-started">
      <Group justify="space-between" align="flex-start" mb="sm" wrap="nowrap">
        <Group gap="sm">
          <ThemeIcon variant="light" size="lg">
            <IconRocket size={20} aria-hidden="true" />
          </ThemeIcon>
          <div>
            <Title order={4} id="getting-started">
              Get started
            </Title>
            <Text size="sm" c="dimmed">
              Three steps and netprobe watches your network.
            </Text>
          </div>
        </Group>
        <CloseButton
          aria-label="Hide the getting started guide"
          onClick={() => {
            try {
              localStorage.setItem(KEY, "1");
            } catch {
              // Storage is blocked: it comes back at the next visit.
            }
            setClosed(true);
          }}
        />
      </Group>
      <Stack gap="sm">
        {steps.map((s, i) => (
          <Group key={s.title} justify="space-between" wrap="nowrap" align="center">
            <Group gap="sm" wrap="nowrap" align="flex-start">
              <ThemeIcon radius="xl" size="md" variant={s.done ? "filled" : "light"} color={s.done ? "green" : "blue"}>
                {s.done ? <IconCheck size={14} aria-label="Done" /> : <Text size="xs" fw={700}>{i + 1}</Text>}
              </ThemeIcon>
              <div>
                <Text fw={500} td={s.done ? "line-through" : undefined} c={s.done ? "dimmed" : undefined}>
                  {s.title}
                </Text>
                <Text size="sm" c="dimmed">
                  {s.text}
                </Text>
              </div>
            </Group>
            {!s.done && (
              <Button component={Link} to={s.to} size="xs" variant={i === next ? "filled" : "default"} rightSection={<IconArrowRight size={14} aria-hidden="true" />}>
                {s.action}
              </Button>
            )}
          </Group>
        ))}
      </Stack>
    </Paper>
  );
}
