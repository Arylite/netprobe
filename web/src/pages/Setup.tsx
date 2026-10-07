import { Box, Button, Center, Code, Group, Loader, Paper, PasswordInput, Stack, Text, TextInput, Title } from "@mantine/core";
import { useForm } from "@mantine/form";
import { IconRadar2, IconUserPlus } from "@tabler/icons-react";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Navigate } from "react-router";

import { errorText } from "../api/client";
import { useSetupStatus } from "../api/queries";
import { useSession } from "../auth/session";
import { ColorSchemeSwitch } from "../components/Layout";
import { ErrorAlert, InfoAlert } from "../components/ui";
import { pageBackground } from "../theme";

/** The first screen of a new central: it has no account, so it asks for the first one. */
export function Setup() {
  const { user, setup } = useSession();
  const queryClient = useQueryClient();
  const status = useSetupStatus();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const form = useForm({
    mode: "uncontrolled",
    initialValues: { code: "", username: "", password: "", confirm: "" },
    validate: {
      code: (v) => (/^\d{6}$/.test(v.replace(/[\s-]/g, "")) ? null : "The code has six digits"),
      username: (v) => (/^[a-z0-9][a-z0-9._-]{0,63}$/.test(v) ? null : "Lowercase letters, digits, dots, dashes or underscores"),
      password: (v) => (v.length < 12 || v.length > 128 ? "12 to 128 characters" : null),
      confirm: (v, values) => (v === values.password ? null : "The two passwords differ"),
    },
  });

  if (user) return <Navigate to="/" replace />;
  if (status.isPending) {
    return (
      <Center mih="100vh" style={{ background: pageBackground }}>
        <Loader aria-label="Loading" />
      </Center>
    );
  }
  // Once an account exists there is nothing to set up.
  if (status.data === false) return <Navigate to="/login" replace />;

  const submit = form.onSubmit(async ({ code, username, password }) => {
    setBusy(true);
    setError(null);
    try {
      await setup(code, username, password);
      void queryClient.invalidateQueries({ queryKey: ["setup"] });
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  });

  return (
    <Center mih="100vh" p="md" style={{ background: pageBackground }}>
      <Box pos="absolute" top={12} right={12}>
        <ColorSchemeSwitch />
      </Box>
      <Paper w="100%" maw={460} p="xl" shadow="sm">
        <form onSubmit={submit} noValidate>
          <Stack gap="md">
            <Group gap="sm">
              <IconRadar2 size={32} aria-hidden="true" />
              <div>
                <Title order={2}>Welcome to netprobe</Title>
                <Text c="dimmed" size="sm">
                  Create the administrator account to get started
                </Text>
              </div>
            </Group>
            <InfoAlert>
              The setup code is six digits, written in the log of the central when it starts. For example:
              <Code block mt="xs">
                docker compose logs central | grep setup_code
              </Code>
            </InfoAlert>
            {error && <ErrorAlert error={error} />}
            <TextInput
              label="Setup code"
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="123456"
              autoFocus
              key={form.key("code")}
              {...form.getInputProps("code")}
            />
            <TextInput label="Username" autoComplete="username" key={form.key("username")} {...form.getInputProps("username")} />
            <PasswordInput
              label="Password"
              description="12 to 128 characters. A sentence works well."
              autoComplete="new-password"
              key={form.key("password")}
              {...form.getInputProps("password")}
            />
            <PasswordInput label="Password, again" autoComplete="new-password" key={form.key("confirm")} {...form.getInputProps("confirm")} />
            <Button type="submit" loading={busy} leftSection={<IconUserPlus size={18} aria-hidden="true" />} fullWidth>
              Create the account
            </Button>
          </Stack>
        </form>
      </Paper>
    </Center>
  );
}
