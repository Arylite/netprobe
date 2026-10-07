import { Badge, Button, Group, Paper, PasswordInput, Stack, Text, Title } from "@mantine/core";
import { useForm } from "@mantine/form";
import { IconKey } from "@tabler/icons-react";
import { useMutation } from "@tanstack/react-query";

import { unwrapEmpty } from "../api/client";
import { useSession, useUser } from "../auth/session";
import { ErrorAlert, PageHeader } from "../components/ui";

export function Account() {
  const { api, endSession } = useSession();
  const user = useUser();

  const form = useForm({
    mode: "uncontrolled",
    initialValues: { current: "", next: "", confirm: "" },
    validate: {
      current: (v) => (v === "" ? "Enter your current password" : null),
      next: (v) => (v.length < 12 || v.length > 128 ? "12 to 128 characters" : null),
      confirm: (v, values) => (v === values.next ? null : "The two passwords differ"),
    },
  });

  const change = useMutation({
    mutationFn: (v: { current: string; next: string }) =>
      unwrapEmpty(api.POST("/api/v1/me/password", { body: { current_password: v.current, new_password: v.next } })),
    // The central ends every session of the account, this one included.
    onSuccess: () => endSession("Your password was changed. Sign in with the new one."),
  });

  return (
    <>
      <PageHeader title="Account" />
      <Stack gap="lg" maw={480}>
        <Paper p="lg">
          <Group justify="space-between">
            <div>
              <Text size="sm" c="dimmed">
                Signed in as
              </Text>
              <Text fw={600} size="lg">
                {user.username}
              </Text>
            </div>
            <Badge variant="light" color={user.role === "admin" ? "blue" : "gray"} size="lg">
              {user.role === "admin" ? "Administrator" : "Viewer"}
            </Badge>
          </Group>
        </Paper>

        <Paper p="lg">
          <form onSubmit={form.onSubmit(({ current, next }) => change.mutate({ current, next }))} noValidate>
            <Stack gap="md">
              <Title order={4}>Change your password</Title>
              <Text size="sm" c="dimmed">
                Every session of your account ends when it is changed, this one included.
              </Text>
              <PasswordInput label="Current password" autoComplete="current-password" key={form.key("current")} {...form.getInputProps("current")} />
              <PasswordInput label="New password" description="12 to 128 characters." autoComplete="new-password" key={form.key("next")} {...form.getInputProps("next")} />
              <PasswordInput label="New password, again" autoComplete="new-password" key={form.key("confirm")} {...form.getInputProps("confirm")} />
              {change.isError && <ErrorAlert error={change.error} />}
              <Group justify="flex-end">
                <Button type="submit" loading={change.isPending} leftSection={<IconKey size={18} aria-hidden="true" />}>
                  Change the password
                </Button>
              </Group>
            </Stack>
          </form>
        </Paper>
      </Stack>
    </>
  );
}
