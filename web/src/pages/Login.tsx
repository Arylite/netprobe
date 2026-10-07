import { Box, Button, Center, Group, Paper, PasswordInput, Stack, Text, TextInput, Title } from "@mantine/core";
import { useForm } from "@mantine/form";
import { IconLogin, IconRadar2 } from "@tabler/icons-react";
import { useState } from "react";
import { Navigate, useLocation } from "react-router";

import { errorText } from "../api/client";
import { useSession } from "../auth/session";
import { ColorSchemeSwitch } from "../components/Layout";
import { pageBackground } from "../theme";
import { ErrorAlert, InfoAlert } from "../components/ui";

/** Only a path inside the app is a place to go back to. */
function destination(state: unknown): string {
  const from = (state as { from?: unknown } | null)?.from;
  return typeof from === "string" && from.startsWith("/") && !from.startsWith("//") && !from.startsWith("/login") ? from : "/";
}

export function Login() {
  const { user, login, notice } = useSession();
  const location = useLocation();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const form = useForm({
    mode: "uncontrolled",
    initialValues: { username: "", password: "" },
    validate: {
      username: (v) => (v.trim() === "" ? "Enter your username" : null),
      password: (v) => (v === "" ? "Enter your password" : null),
    },
  });

  if (user) return <Navigate to={destination(location.state)} replace />;

  const submit = form.onSubmit(async ({ username, password }) => {
    setBusy(true);
    setError(null);
    try {
      await login(username.trim(), password);
    } catch (err) {
      setError(errorText(err));
      form.setFieldValue("password", "");
    } finally {
      setBusy(false);
    }
  });

  return (
    <Center mih="100vh" p="md" style={{ background: pageBackground }}>
      <Box pos="absolute" top={12} right={12}>
        <ColorSchemeSwitch />
      </Box>
      <Paper w="100%" maw={400} p="xl" shadow="sm">
        <form onSubmit={submit} noValidate>
          <Stack gap="md">
            <Group gap="sm">
              <IconRadar2 size={32} aria-hidden="true" />
              <div>
                <Title order={2}>netprobe</Title>
                <Text c="dimmed" size="sm">
                  Sign in to manage your probes
                </Text>
              </div>
            </Group>
            {notice && <InfoAlert>{notice}</InfoAlert>}
            {error && <ErrorAlert error={error} />}
            <TextInput label="Username" autoComplete="username" autoFocus key={form.key("username")} {...form.getInputProps("username")} />
            <PasswordInput label="Password" autoComplete="current-password" key={form.key("password")} {...form.getInputProps("password")} />
            <Button type="submit" loading={busy} leftSection={<IconLogin size={18} aria-hidden="true" />} fullWidth>
              Sign in
            </Button>
          </Stack>
        </form>
      </Paper>
    </Center>
  );
}
