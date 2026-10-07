import { Alert, Anchor, Group, Paper, SimpleGrid, Stack, Table, Text } from "@mantine/core";
import { IconBellRinging } from "@tabler/icons-react";
import { Link } from "react-router";

import type { CheckStatus } from "../api/client";
import { useEdges, useIncidents, useStatus } from "../api/queries";
import { GettingStarted } from "../components/GettingStarted";
import { DataTable, PageHeader, QueryState, StateBadge, Time } from "../components/ui";
import { percent, rtt } from "../lib/format";
import { subject } from "../lib/incident";
import { runState } from "../lib/state";

function Figure({ label, value, hint, tone }: { label: string; value: number | string; hint?: string; tone?: "red" | "green" }) {
  return (
    <Paper p="md">
      <Text size="sm" c="dimmed">
        {label}
      </Text>
      <Text fz={32} fw={700} lh={1.2} c={tone}>
        {value}
      </Text>
      {hint && (
        <Text size="xs" c="dimmed">
          {hint}
        </Text>
      )}
    </Paper>
  );
}

function failingNow(checks: CheckStatus[]): number {
  const now = Date.now();
  return checks.filter((c) => c.edges.some((e) => runState(e.last_at, e.ok, c.interval_seconds, now) === "failing")).length;
}

export function Overview() {
  const status = useStatus();
  const incidents = useIncidents("open");
  const edges = useEdges();

  const open = incidents.data ?? [];
  const activeEdges = (edges.data ?? []).filter((e) => !e.revoked_at).length;

  return (
    <>
      <PageHeader title="Overview" description="How every check does, on each edge, over the last 24 hours." />
      <Stack gap="lg">
        <GettingStarted />
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <Figure label="Open incidents" value={incidents.data ? open.length : "-"} tone={open.length > 0 ? "red" : undefined} />
          <Figure label="Active edges" value={edges.data ? activeEdges : "-"} />
          <Figure
            label="Checks failing now"
            value={status.data ? failingNow(status.data.checks) : "-"}
            hint={status.data ? `of ${status.data.checks.length}` : undefined}
            tone={status.data && failingNow(status.data.checks) > 0 ? "red" : undefined}
          />
        </SimpleGrid>

        {open.length > 0 && (
          <Alert color="red" variant="light" icon={<IconBellRinging size={18} aria-hidden="true" />} title={`${open.length} open ${open.length === 1 ? "incident" : "incidents"}`}>
            <Stack gap={4}>
              {open.slice(0, 5).map((i) => (
                <Group key={i.id} gap="xs">
                  <Text size="sm" fw={500}>
                    {subject(i)}
                  </Text>
                  <Text size="sm" c="dimmed">
                    since <Time iso={i.started_at} />
                  </Text>
                </Group>
              ))}
              <Anchor component={Link} to="/incidents" size="sm" fw={500} c="inherit" underline="always" mt="xs" w="fit-content">
                See the incidents
              </Anchor>
            </Stack>
          </Alert>
        )}

        <QueryState query={status}>
          {(data) => (
            <DataTable head={["Check", "Edge", "State", "Last run", "Response", "Success (24 h)", "p95"]} empty={data.checks.length === 0 ? "No check yet. Add one in Checks." : undefined}>
              {data.checks.flatMap((c) =>
                c.edges.length === 0
                  ? [
                      <Table.Tr key={c.id}>
                        <Table.Td>
                          <Anchor component={Link} to={`/checks/${encodeURIComponent(c.id)}`} fw={500}>
                            {c.id}
                          </Anchor>
                        </Table.Td>
                        <Table.Td c="dimmed" colSpan={6}>
                          No result in the last 24 hours
                        </Table.Td>
                      </Table.Tr>,
                    ]
                  : c.edges.map((e) => {
                      const state = runState(e.last_at, e.ok, c.interval_seconds);
                      return (
                        <Table.Tr key={`${c.id}/${e.edge_id}`}>
                          <Table.Td>
                            <Anchor component={Link} to={`/checks/${encodeURIComponent(c.id)}`} fw={500}>
                              {c.id}
                            </Anchor>
                          </Table.Td>
                          <Table.Td>{e.edge}</Table.Td>
                          <Table.Td>
                            {state === "ok" && <StateBadge tone="ok">OK</StateBadge>}
                            {state === "failing" && <StateBadge tone="bad">Failing</StateBadge>}
                            {state === "stale" && <StateBadge tone="warn">No recent run</StateBadge>}
                          </Table.Td>
                          <Table.Td>
                            <Time iso={e.last_at} />
                          </Table.Td>
                          <Table.Td>{e.ok ? rtt(e.rtt_millis) : <Text span c="red" size="sm">{e.error ?? "failed"}</Text>}</Table.Td>
                          <Table.Td>{percent(e.success_ratio)}</Table.Td>
                          <Table.Td>{e.p95_rtt_millis === undefined ? "-" : rtt(e.p95_rtt_millis)}</Table.Td>
                        </Table.Tr>
                      );
                    }),
              )}
            </DataTable>
          )}
        </QueryState>
      </Stack>
    </>
  );
}
