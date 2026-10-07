import { Table } from "@mantine/core";
import type { ReactNode } from "react";

/** The cell of a table that holds the actions of a row, kept to the right. */
export function ActionsCell({ children }: { children: ReactNode }) {
  return (
    <Table.Td ta="right" style={{ whiteSpace: "nowrap" }}>
      {children}
    </Table.Td>
  );
}
