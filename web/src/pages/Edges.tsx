import { Button, Code, Group, Modal, Stack, Table, Text, TextInput } from "@mantine/core";
import { useForm } from "@mantine/form";
import { useDisclosure } from "@mantine/hooks";
import { IconBan, IconPlus } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { unwrap, unwrapEmpty, type Edge, type Schemas } from "../api/client";
import { useEdges } from "../api/queries";
import { useSession, useUser } from "../auth/session";
import { ActionsCell } from "../components/ActionsCell";
import { DataTable, ErrorAlert, PageHeader, QueryState, SecretDialog, StateBadge, Time, confirmAction, fail, succeed } from "../components/ui";

const NAME = /^[a-z0-9][a-z0-9-]{0,62}$/;
const QUIET_AFTER_MS = 5 * 60 * 1000;

function EdgeState({ edge }: { edge: Edge }) {
  if (edge.revoked_at) return <StateBadge tone="muted">Revoked</StateBadge>;
  if (!edge.last_seen) return <StateBadge tone="muted">Never reported</StateBadge>;
  const quiet = Date.now() - Date.parse(edge.last_seen) > QUIET_AFTER_MS;
  return quiet ? <StateBadge tone="warn">Silent</StateBadge> : <StateBadge tone="ok">Reporting</StateBadge>;
}

export function Edges() {
  const { api } = useSession();
  const admin = useUser().role === "admin";
  const queryClient = useQueryClient();
  const edges = useEdges();
  const [adding, { open, close }] = useDisclosure(false);
  const [created, setCreated] = useState<Schemas["CreatedEdge"] | null>(null);

  const form = useForm({
    mode: "uncontrolled",
    initialValues: { name: "" },
    validate: { name: (v) => (NAME.test(v) ? null : "Use 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit") },
  });

  const create = useMutation({
    mutationFn: (name: string) => unwrap(api.POST("/api/v1/edges", { body: { name } })),
    onSuccess: (edge) => {
      void queryClient.invalidateQueries({ queryKey: ["edges"] });
      close();
      form.reset();
      setCreated(edge);
    },
  });

  const revoke = useMutation({
    mutationFn: (name: string) => unwrapEmpty(api.DELETE("/api/v1/edges/{name}", { params: { path: { name } } })),
    onSuccess: (_, name) => {
      void queryClient.invalidateQueries({ queryKey: ["edges"] });
      succeed(`Edge ${name} revoked`);
    },
    onError: (err) => fail(err, "The edge was not revoked"),
  });

  return (
    <>
      <PageHeader title="Edges" description="The machines that run the checks and report to this central.">
        {admin && (
          <Button
            leftSection={<IconPlus size={18} aria-hidden="true" />}
            onClick={() => {
              create.reset();
              open();
            }}
          >
            Add an edge
          </Button>
        )}
      </PageHeader>

      <QueryState query={edges}>
        {(rows) => (
          <DataTable
            head={admin ? ["Name", "State", "Last report", "Added", ""] : ["Name", "State", "Last report", "Added"]}
            empty={rows.length === 0 ? "No edge yet." : undefined}
          >
            {rows.map((e) => (
              <Table.Tr key={e.id}>
                <Table.Td fw={500}>{e.name}</Table.Td>
                <Table.Td>
                  <EdgeState edge={e} />
                </Table.Td>
                <Table.Td>
                  {e.last_seen ? (
                    <Time iso={e.last_seen} />
                  ) : (
                    <Text span c="dimmed">
                      -
                    </Text>
                  )}
                </Table.Td>
                <Table.Td>
                  <Time iso={e.created_at} />
                </Table.Td>
                {admin && (
                  <ActionsCell>
                    {!e.revoked_at && (
                      <Button
                        variant="subtle"
                        color="red"
                        size="xs"
                        leftSection={<IconBan size={16} aria-hidden="true" />}
                        aria-label={`Revoke ${e.name}`}
                        loading={revoke.isPending && revoke.variables === e.name}
                        onClick={() =>
                          confirmAction({
                            title: `Revoke ${e.name}?`,
                            message: "The edge can no longer poll or report. This cannot be undone: register a new edge to replace it.",
                            confirmLabel: "Revoke",
                            onConfirm: () => revoke.mutate(e.name),
                          })
                        }
                      >
                        Revoke
                      </Button>
                    )}
                  </ActionsCell>
                )}
              </Table.Tr>
            ))}
          </DataTable>
        )}
      </QueryState>

      <Modal opened={adding} onClose={close} title="Add an edge" centered>
        <form onSubmit={form.onSubmit((v) => create.mutate(v.name))} noValidate>
          <Stack gap="md">
            <TextInput
              label="Name"
              description="The machine or place it measures from, for example paris."
              data-autofocus
              key={form.key("name")}
              {...form.getInputProps("name")}
            />
            {create.isError && <ErrorAlert error={create.error} />}
            <Group justify="flex-end">
              <Button variant="default" onClick={close}>
                Cancel
              </Button>
              <Button type="submit" loading={create.isPending}>
                Add
              </Button>
            </Group>
          </Stack>
        </form>
      </Modal>

      <SecretDialog opened={created !== null} title={`Token of ${created?.name ?? ""}`} secret={created?.token ?? ""} onClose={() => setCreated(null)}>
        <Text size="sm">Give it to the edge in its environment, never on a command line:</Text>
        <Code block>NETPROBE_TOKEN=&lt;the token&gt; netprobe-edge --central https://your-central</Code>
      </SecretDialog>
    </>
  );
}
