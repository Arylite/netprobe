import { Badge, Button, Group, Modal, PasswordInput, Select, Stack, Table, TextInput, Tooltip } from "@mantine/core";
import { useForm } from "@mantine/form";
import { useDisclosure } from "@mantine/hooks";
import { IconPlus, IconTrash } from "@tabler/icons-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { unwrap, unwrapEmpty } from "../api/client";
import { useUsers } from "../api/queries";
import { useSession, useUser } from "../auth/session";
import { ActionsCell } from "../components/ActionsCell";
import { DataTable, ErrorAlert, PageHeader, QueryState, confirmAction, fail, succeed } from "../components/ui";

const ROLES = [
  { value: "viewer", label: "Viewer: reads everything" },
  { value: "admin", label: "Administrator: also manages" },
];

export function Users() {
  const { api } = useSession();
  const self = useUser();
  const queryClient = useQueryClient();
  const users = useUsers();
  const [adding, { open, close }] = useDisclosure(false);

  const form = useForm({
    mode: "uncontrolled",
    initialValues: { username: "", role: "viewer", password: "" },
    validate: {
      username: (v) => (v.trim() === "" ? "Choose a username" : null),
      password: (v) => (v.length < 12 || v.length > 128 ? "12 to 128 characters" : null),
    },
  });

  const create = useMutation({
    mutationFn: (v: { username: string; role: string; password: string }) =>
      unwrap(api.POST("/api/v1/users", { body: { username: v.username.trim(), role: v.role as "admin" | "viewer", password: v.password } })),
    onSuccess: (user) => {
      void queryClient.invalidateQueries({ queryKey: ["users"] });
      close();
      form.reset();
      succeed(`Account ${user.username} created`);
    },
  });

  const remove = useMutation({
    mutationFn: (username: string) => unwrapEmpty(api.DELETE("/api/v1/users/{username}", { params: { path: { username } } })),
    onSuccess: (_, username) => {
      void queryClient.invalidateQueries({ queryKey: ["users"] });
      succeed(`Account ${username} deleted`);
    },
    onError: (err) => fail(err, "The account was not deleted"),
  });

  return (
    <>
      <PageHeader title="Users" description="Who can sign in. A viewer reads everything, an administrator also manages.">
        <Button
          leftSection={<IconPlus size={18} aria-hidden="true" />}
          onClick={() => {
            create.reset();
            open();
          }}
        >
          Add a user
        </Button>
      </PageHeader>

      <QueryState query={users}>
        {(rows) => (
          <DataTable head={["Username", "Role", ""]}>
            {rows.map((u) => (
              <Table.Tr key={u.username}>
                <Table.Td fw={500}>{u.username}</Table.Td>
                <Table.Td>
                  <Badge variant="light" color={u.role === "admin" ? "blue" : "gray"}>
                    {u.role === "admin" ? "Administrator" : "Viewer"}
                  </Badge>
                </Table.Td>
                <ActionsCell>
                  <Tooltip label="You cannot delete your own account" disabled={u.username !== self.username}>
                    <span>
                      <Button
                        variant="subtle"
                        color="red"
                        size="xs"
                        leftSection={<IconTrash size={16} aria-hidden="true" />}
                        aria-label={`Delete ${u.username}`}
                        disabled={u.username === self.username}
                        loading={remove.isPending && remove.variables === u.username}
                        onClick={() =>
                          confirmAction({
                            title: `Delete ${u.username}?`,
                            message: "The account and its sessions end now.",
                            confirmLabel: "Delete",
                            onConfirm: () => remove.mutate(u.username),
                          })
                        }
                      >
                        Delete
                      </Button>
                    </span>
                  </Tooltip>
                </ActionsCell>
              </Table.Tr>
            ))}
          </DataTable>
        )}
      </QueryState>

      <Modal opened={adding} onClose={close} title="Add a user" centered>
        <form onSubmit={form.onSubmit((v) => create.mutate(v))} noValidate>
          <Stack gap="md">
            <TextInput label="Username" autoComplete="off" data-autofocus key={form.key("username")} {...form.getInputProps("username")} />
            <Select label="Role" data={ROLES} allowDeselect={false} key={form.key("role")} {...form.getInputProps("role")} />
            <PasswordInput
              label="Password"
              description="12 to 128 characters. The person can change it from their account."
              autoComplete="new-password"
              key={form.key("password")}
              {...form.getInputProps("password")}
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
