import { Badge, Select, Table, Text } from "@mantine/core";
import { useState } from "react";

import { useAudit } from "../api/queries";
import { DataTable, PageHeader, QueryState, Time } from "../components/ui";

const LIMITS = ["100", "250", "500"];

/** How an action reads on screen; the rest show as they are. */
const LABELS: Record<string, string> = {
  login: "Signed in",
  "login.failed": "Sign-in refused",
  logout: "Signed out",
  setup: "First administrator created",
  "password.change": "Password changed",
  "edge.create": "Edge added",
  "edge.revoke": "Edge revoked",
  "check.create": "Check added",
  "check.remove": "Check removed",
  "channel.create": "Channel added",
  "channel.remove": "Channel removed",
  "channel.test": "Channel tested",
  "user.create": "User added",
  "user.delete": "User deleted",
};

export function Audit() {
  const [limit, setLimit] = useState("100");
  const events = useAudit(Number(limit));
  return (
    <>
      <PageHeader title="Audit log" description="Who signed in and who changed what, with the address it came from. Kept for a year by default.">
        <Select aria-label="How many events" data={LIMITS} value={limit} onChange={(v) => v && setLimit(v)} allowDeselect={false} w={110} />
      </PageHeader>
      <QueryState query={events}>
        {(rows) => (
          <DataTable head={["When", "Who", "What", "On", "From"]} empty={rows.length === 0 ? "Nothing has happened yet." : undefined}>
            {rows.map((e) => (
              <Table.Tr key={e.id}>
                <Table.Td>
                  <Time iso={e.at} />
                </Table.Td>
                <Table.Td fw={500}>{e.actor ?? <Text span c="dimmed">nobody</Text>}</Table.Td>
                <Table.Td>
                  <Badge variant="light" color={e.action === "login.failed" ? "red" : "gray"} tt="none">
                    {LABELS[e.action] ?? e.action}
                  </Badge>
                </Table.Td>
                <Table.Td>{e.target ?? ""}</Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed" ff="monospace">
                    {e.client_ip ?? ""}
                  </Text>
                </Table.Td>
              </Table.Tr>
            ))}
          </DataTable>
        )}
      </QueryState>
    </>
  );
}
