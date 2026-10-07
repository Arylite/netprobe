import { Button, Group, Modal, PasswordInput, Stack, Table, Text, TextInput } from "@mantine/core";
import { useForm } from "@mantine/form";
import { useDisclosure } from "@mantine/hooks";
import { IconPlus, IconSend, IconTrash } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { unwrap, unwrapEmpty } from "../api/client";
import { useChannels } from "../api/queries";
import { useSession } from "../auth/session";
import { ActionsCell } from "../components/ActionsCell";
import { DataTable, ErrorAlert, PageHeader, QueryState, StateBadge, Time, confirmAction, fail, succeed } from "../components/ui";

const NAME = /^[a-z0-9][a-z0-9-]{0,62}$/;

function validUrl(v: string): boolean {
  try {
    const u = new URL(v);
    return (u.protocol === "http:" || u.protocol === "https:") && u.username === "" && u.password === "";
  } catch {
    return false;
  }
}

export function Channels() {
  const { api } = useSession();
  const queryClient = useQueryClient();
  const channels = useChannels();
  const [adding, { open, close }] = useDisclosure(false);

  const form = useForm({
    mode: "uncontrolled",
    initialValues: { name: "", url: "", secret: "" },
    validate: {
      name: (v) => (NAME.test(v) ? null : "Use 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit"),
      url: (v) => (validUrl(v) ? null : "An http:// or https:// address, without a user name or a password"),
      secret: (v) => (v.length > 256 ? "At most 256 characters" : null),
    },
  });

  const create = useMutation({
    mutationFn: (v: { name: string; url: string; secret: string }) =>
      unwrap(api.POST("/api/v1/channels", { body: { name: v.name, url: v.url, ...(v.secret ? { secret: v.secret } : {}) } })),
    onSuccess: (channel) => {
      void queryClient.invalidateQueries({ queryKey: ["channels"] });
      close();
      form.reset();
      succeed(`Channel ${channel.name} added: it hears of the incidents that start from now on`);
    },
  });

  const test = useMutation({
    mutationFn: (name: string) => unwrapEmpty(api.POST("/api/v1/channels/{name}/test", { params: { path: { name } } })),
    onSuccess: (_, name) => succeed(`${name} accepted the test notification`),
    onError: (err, name) => fail(err, `${name} did not accept the test`),
  });

  const remove = useMutation({
    mutationFn: (name: string) => unwrapEmpty(api.DELETE("/api/v1/channels/{name}", { params: { path: { name } } })),
    onSuccess: (_, name) => {
      void queryClient.invalidateQueries({ queryKey: ["channels"] });
      succeed(`Channel ${name} removed`);
    },
    onError: (err) => fail(err, "The channel was not removed"),
  });

  return (
    <>
      <PageHeader title="Channels" description="Webhooks told when an incident opens, and when it resolves. The body is JSON with a text field, which chat tools accept as it is.">
        <Button
          leftSection={<IconPlus size={18} aria-hidden="true" />}
          onClick={() => {
            create.reset();
            open();
          }}
        >
          Add a channel
        </Button>
      </PageHeader>

      <QueryState query={channels}>
        {(rows) => (
          <DataTable head={["Name", "Address", "Signed", "Added", ""]} empty={rows.length === 0 ? "No channel yet: incidents are recorded, but nobody is told." : undefined}>
            {rows.map((c) => (
              <Table.Tr key={c.name}>
                <Table.Td fw={500}>{c.name}</Table.Td>
                <Table.Td>
                  <Text ff="monospace" size="sm" style={{ wordBreak: "break-all" }}>
                    {c.url}
                  </Text>
                </Table.Td>
                <Table.Td>{c.has_secret ? <StateBadge tone="ok">Signed</StateBadge> : <StateBadge tone="muted">Not signed</StateBadge>}</Table.Td>
                <Table.Td>
                  <Time iso={c.created_at} />
                </Table.Td>
                <ActionsCell>
                  <Group gap="xs" justify="flex-end" wrap="nowrap">
                    <Button
                      variant="default"
                      size="xs"
                      leftSection={<IconSend size={16} aria-hidden="true" />}
                      aria-label={`Send a test to ${c.name}`}
                      loading={test.isPending && test.variables === c.name}
                      onClick={() => test.mutate(c.name)}
                    >
                      Test
                    </Button>
                    <Button
                      variant="subtle"
                      color="red"
                      size="xs"
                      leftSection={<IconTrash size={16} aria-hidden="true" />}
                      aria-label={`Remove ${c.name}`}
                      loading={remove.isPending && remove.variables === c.name}
                      onClick={() =>
                        confirmAction({
                          title: `Remove ${c.name}?`,
                          message: "It is no longer told of incidents.",
                          confirmLabel: "Remove",
                          onConfirm: () => remove.mutate(c.name),
                        })
                      }
                    >
                      Remove
                    </Button>
                  </Group>
                </ActionsCell>
              </Table.Tr>
            ))}
          </DataTable>
        )}
      </QueryState>

      <Modal opened={adding} onClose={close} title="Add a channel" centered size="lg">
        <form onSubmit={form.onSubmit((v) => create.mutate(v))} noValidate>
          <Stack gap="md">
            <TextInput label="Name" data-autofocus key={form.key("name")} {...form.getInputProps("name")} />
            <TextInput
              label="Webhook address"
              description="Receives a JSON POST. It may hold a secret of its own: only administrators see it."
              placeholder="https://hooks.example.com/..."
              key={form.key("url")}
              {...form.getInputProps("url")}
            />
            <PasswordInput
              label="Signing secret (optional)"
              description="Signs each notification, so that the receiver can check it comes from netprobe. It is never shown again."
              autoComplete="off"
              key={form.key("secret")}
              {...form.getInputProps("secret")}
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
    </>
  );
}
