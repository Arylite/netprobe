import { SegmentedControl, Table, Text } from "@mantine/core";
import { useState } from "react";

import { useIncidents } from "../api/queries";
import { DataTable, PageHeader, QueryState, StateBadge, Time } from "../components/ui";
import { lasted, subject } from "../lib/incident";

type Filter = "all" | "open";

export function Incidents() {
  const [filter, setFilter] = useState<Filter>("all");
  const incidents = useIncidents(filter);
  return (
    <>
      <PageHeader title="Incidents" description="A check that keeps failing on an edge, or an edge that stopped reporting.">
        <SegmentedControl
          aria-label="Which incidents"
          value={filter}
          onChange={(v) => setFilter(v as Filter)}
          data={[
            { value: "all", label: "All" },
            { value: "open", label: "Open" },
          ]}
        />
      </PageHeader>
      <QueryState query={incidents}>
        {(rows) => (
          <DataTable
            head={["State", "Subject", "Started", "Duration", "Detail"]}
            empty={rows.length === 0 ? (filter === "open" ? "Nothing is wrong right now." : "No incident has been recorded.") : undefined}
          >
            {rows.map((i) => (
              <Table.Tr key={i.id}>
                <Table.Td>
                  {i.resolved_at ? (
                    <StateBadge tone="ok">{i.resolution ? `Resolved (${i.resolution})` : "Resolved"}</StateBadge>
                  ) : (
                    <StateBadge tone="bad">Open</StateBadge>
                  )}
                </Table.Td>
                <Table.Td fw={500}>{subject(i)}</Table.Td>
                <Table.Td>
                  <Time iso={i.started_at} />
                </Table.Td>
                <Table.Td>{lasted(i)}</Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {i.detail ?? ""}
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
