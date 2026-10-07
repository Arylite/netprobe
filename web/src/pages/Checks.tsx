import { Anchor, Badge, Button, Group, Modal, NumberInput, Select, Stack, Table, Text, TextInput } from "@mantine/core";
import { useForm } from "@mantine/form";
import { useDisclosure } from "@mantine/hooks";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";

import { unwrap, unwrapEmpty } from "../api/client";
import { useChecks } from "../api/queries";
import { useSession, useUser } from "../auth/session";
import { ActionsCell } from "../components/ActionsCell";
import { DataTable, ErrorAlert, PageHeader, QueryState, confirmAction, fail, succeed } from "../components/ui";

const KINDS = [
  { value: "tcp", label: "TCP connect" },
  { value: "http", label: "HTTP request" },
];

const TARGET_HINT: Record<string, string> = {
  tcp: "host:port, for example example.com:443",
  http: "A URL, for example https://example.com/health",
};

export function Checks() {
  const { api } = useSession();
  const admin = useUser().role === "admin";
  const queryClient = useQueryClient();
  const checks = useChecks();
  const [adding, { open, close }] = useDisclosure(false);

  const form = useForm({
    mode: "controlled",
    initialValues: { id: "", kind: "tcp", target: "", interval: 30 as number | string },
    validate: {
      id: (v) => (v.trim() === "" ? "Give the check a name" : null),
      target: (v) => (v.trim() === "" ? "Say what to measure" : null),
      interval: (v) => (typeof v === "number" && v >= 1 ? null : "At least 1 second"),
    },
  });

  const create = useMutation({
    mutationFn: (v: typeof form.values) =>
      unwrap(api.POST("/api/v1/checks", { body: { id: v.id.trim(), kind: v.kind as "tcp" | "http", target: v.target.trim(), interval_seconds: Number(v.interval) } })),
    onSuccess: (check) => {
      void queryClient.invalidateQueries({ queryKey: ["checks"] });
      void queryClient.invalidateQueries({ queryKey: ["status"] });
      close();
      form.reset();
      succeed(`Check ${check.id} added: every edge runs it from its next poll`);
    },
  });

  const remove = useMutation({
    mutationFn: (id: string) => unwrapEmpty(api.DELETE("/api/v1/checks/{id}", { params: { path: { id } } })),
    onSuccess: (_, id) => {
      void queryClient.invalidateQueries({ queryKey: ["checks"] });
      void queryClient.invalidateQueries({ queryKey: ["status"] });
      succeed(`Check ${id} removed`);
    },
    onError: (err) => fail(err, "The check was not removed"),
  });

  return (
    <>
      <PageHeader title="Checks" description="What every edge measures, and how often.">
        {admin && (
          <Button
            leftSection={<IconPlus size={18} aria-hidden="true" />}
            onClick={() => {
              create.reset();
              open();
            }}
          >
            Add a check
          </Button>
        )}
      </PageHeader>

      <QueryState query={checks}>
        {(rows) => (
          <DataTable head={admin ? ["Name", "Kind", "Target", "Every", ""] : ["Name", "Kind", "Target", "Every"]} empty={rows.length === 0 ? "No check yet." : undefined}>
            {rows.map((c) => (
              <Table.Tr key={c.id}>
                <Table.Td>
                  <Anchor component={Link} to={`/checks/${encodeURIComponent(c.id)}`} fw={500}>
                    {c.id}
                  </Anchor>
                </Table.Td>
                <Table.Td>
                  <Badge variant="default" tt="uppercase">
                    {c.kind}
                  </Badge>
                </Table.Td>
                <Table.Td>
                  <Text ff="monospace" size="sm" style={{ wordBreak: "break-all" }}>
                    {c.target}
                  </Text>
                </Table.Td>
                <Table.Td>{c.interval_seconds} s</Table.Td>
                {admin && (
                  <ActionsCell>
                    <Button
                      variant="subtle"
                      color="red"
                      size="xs"
                      leftSection={<IconTrash size={16} aria-hidden="true" />}
                      aria-label={`Remove ${c.id}`}
                      loading={remove.isPending && remove.variables === c.id}
                      onClick={() =>
                        confirmAction({
                          title: `Remove ${c.id}?`,
                          message: "The edges stop running it. Its results are kept.",
                          confirmLabel: "Remove",
                          onConfirm: () => remove.mutate(c.id),
                        })
                      }
                    >
                      Remove
                    </Button>
                  </ActionsCell>
                )}
              </Table.Tr>
            ))}
          </DataTable>
        )}
      </QueryState>

      <Modal opened={adding} onClose={close} title="Add a check" centered>
        <form onSubmit={form.onSubmit((v) => create.mutate(v))} noValidate>
          <Stack gap="md">
            <TextInput label="Name" description="Shown in the overview and in notifications." data-autofocus {...form.getInputProps("id")} />
            <Select label="Kind" data={KINDS} allowDeselect={false} {...form.getInputProps("kind")} />
            <TextInput label="Target" description={TARGET_HINT[form.values.kind]} {...form.getInputProps("target")} />
            <NumberInput label="Run every (seconds)" min={1} allowDecimal={false} {...form.getInputProps("interval")} />
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
    </>
  );
}
