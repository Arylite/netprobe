import { Anchor, Group, Select, Table, Text } from "@mantine/core";
import { IconArrowLeft } from "@tabler/icons-react";
import { useState } from "react";
import { Link, useParams } from "react-router";

import { useEdges, useResults } from "../api/queries";
import { DataTable, PageHeader, QueryState, StateBadge, Time } from "../components/ui";
import { rtt } from "../lib/format";

const LIMITS = ["50", "100", "500", "1000"];

export function CheckResults() {
  const { id = "" } = useParams();
  const [limit, setLimit] = useState("100");
  const results = useResults(id, Number(limit));
  const edges = useEdges();
  const names = new Map((edges.data ?? []).map((e) => [e.id, e.name]));

  return (
    <>
      <Anchor component={Link} to="/checks" size="sm" mb="sm" display="inline-block">
        <Group gap={4} wrap="nowrap">
          <IconArrowLeft size={16} aria-hidden="true" /> Checks
        </Group>
      </Anchor>
      <PageHeader title={id} description="The latest results of this check, newest first. Results of a removed check are kept.">
        <Select aria-label="How many results" data={LIMITS} value={limit} onChange={(v) => v && setLimit(v)} allowDeselect={false} w={110} />
      </PageHeader>
      <QueryState query={results}>
        {(rows) => (
          <DataTable head={["When", "Edge", "Result", "Response", "Error"]} empty={rows.length === 0 ? "No result for this check yet." : undefined}>
            {rows.map((r) => (
              <Table.Tr key={`${r.edge_id}/${r.at}`}>
                <Table.Td>
                  <Time iso={r.at} />
                </Table.Td>
                <Table.Td>{names.get(r.edge_id) ?? r.edge_id}</Table.Td>
                <Table.Td>{r.ok ? <StateBadge tone="ok">OK</StateBadge> : <StateBadge tone="bad">Failed</StateBadge>}</Table.Td>
                <Table.Td>{rtt(r.rtt_millis)}</Table.Td>
                <Table.Td>
                  <Text size="sm" c="dimmed">
                    {r.error ?? ""}
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
