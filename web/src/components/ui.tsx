import {
  Alert,
  Badge,
  Button,
  Code,
  CopyButton,
  Group,
  Loader,
  Modal,
  Paper,
  Stack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { modals } from "@mantine/modals";
import { notifications } from "@mantine/notifications";
import {
  IconAlertCircle,
  IconAlertTriangle,
  IconCheck,
  IconCircleCheck,
  IconCircleDashed,
  IconCopy,
  IconInfoCircle,
  IconX,
} from "@tabler/icons-react";
import type { UseQueryResult } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { errorText } from "../api/client";
import { fullDate, relative } from "../lib/format";

export function PageHeader({ title, description, children }: { title: string; description?: string; children?: ReactNode }) {
  return (
    <Group justify="space-between" align="flex-start" mb="lg" wrap="wrap">
      <div>
        <Title order={2}>{title}</Title>
        {description && (
          <Text c="dimmed" size="sm" mt={4}>
            {description}
          </Text>
        )}
      </div>
      {children && <Group gap="sm">{children}</Group>}
    </Group>
  );
}

/** A date as "5 min ago", with the exact time on hover and for assistive tools. */
export function Time({ iso }: { iso: string }) {
  return (
    <time dateTime={iso} title={fullDate(iso)}>
      {relative(iso)}
    </time>
  );
}

const TONES = {
  ok: { color: "green", icon: IconCircleCheck },
  bad: { color: "red", icon: IconAlertTriangle },
  warn: { color: "orange", icon: IconAlertCircle },
  muted: { color: "gray", icon: IconCircleDashed },
} as const;

export type Tone = keyof typeof TONES;

/** A state in words and an icon, so that colour is never what carries it. */
export function StateBadge({ tone, children }: { tone: Tone; children: ReactNode }) {
  const { color, icon: Icon } = TONES[tone];
  return (
    <Badge color={color} variant="light" leftSection={<Icon size={14} aria-hidden="true" />}>
      {children}
    </Badge>
  );
}

export function ErrorAlert({ error }: { error: unknown }) {
  return (
    <Alert color="red" variant="light" icon={<IconAlertCircle size={18} aria-hidden="true" />} role="alert">
      {errorText(error)}
    </Alert>
  );
}

export function InfoAlert({ children, onClose }: { children: ReactNode; onClose?: () => void }) {
  return (
    <Alert color="blue" variant="light" icon={<IconInfoCircle size={18} aria-hidden="true" />} withCloseButton={!!onClose} onClose={onClose} role="status">
      {children}
    </Alert>
  );
}

/** Loading and failure of a query; the data stays on screen while it refreshes. */
export function QueryState<T>({ query, children }: { query: UseQueryResult<T>; children: (data: T) => ReactNode }) {
  if (query.data !== undefined) {
    return (
      <Stack gap="md">
        {query.isError && <ErrorAlert error={query.error} />}
        {children(query.data)}
      </Stack>
    );
  }
  if (query.isError) {
    return (
      <Stack gap="sm" align="flex-start">
        <ErrorAlert error={query.error} />
        <Button variant="default" onClick={() => void query.refetch()}>
          Try again
        </Button>
      </Stack>
    );
  }
  return (
    <Group gap="sm" role="status">
      <Loader size="sm" />
      <Text c="dimmed">Loading...</Text>
    </Group>
  );
}

/** A table in a card that scrolls sideways on a narrow screen instead of breaking the page. */
export function DataTable({ head, children, empty }: { head: string[]; children: ReactNode; empty?: ReactNode }) {
  return (
    <Paper>
      <Table.ScrollContainer minWidth={640}>
        <Table>
          <Table.Thead>
            <Table.Tr>
              {head.map((h) => (
                <Table.Th key={h}>{h}</Table.Th>
              ))}
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>{children}</Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      {empty && (
        <Text c="dimmed" ta="center" py="xl">
          {empty}
        </Text>
      )}
    </Paper>
  );
}

export function succeed(message: string) {
  notifications.show({ color: "green", icon: <IconCheck size={18} aria-hidden="true" />, message, autoClose: 4000 });
}

export function fail(err: unknown, title = "It did not work") {
  notifications.show({ color: "red", icon: <IconX size={18} aria-hidden="true" />, title, message: errorText(err), autoClose: 8000 });
}

/** Asks before a destructive action, in a dialog the keyboard can answer. */
export function confirmAction({
  title,
  message,
  confirmLabel,
  onConfirm,
}: {
  title: string;
  message: ReactNode;
  confirmLabel: string;
  onConfirm: () => void;
}) {
  modals.openConfirmModal({
    title,
    centered: true,
    children: <Text size="sm">{message}</Text>,
    labels: { confirm: confirmLabel, cancel: "Cancel" },
    confirmProps: { color: "red" },
    onConfirm,
  });
}

/** Shows a secret once, with the means to copy it. */
export function SecretDialog({
  opened,
  title,
  secret,
  children,
  onClose,
}: {
  opened: boolean;
  title: string;
  secret: string;
  children?: ReactNode;
  onClose: () => void;
}) {
  return (
    <Modal opened={opened} onClose={onClose} title={title} centered size="lg" closeOnClickOutside={false} closeOnEscape={false} withCloseButton={false}>
      <Stack gap="md">
        <Alert color="orange" variant="light" icon={<IconAlertTriangle size={18} aria-hidden="true" />}>
          Copy it now: it is not shown again.
        </Alert>
        <Group gap="sm" wrap="nowrap" align="center">
          <Code block style={{ flex: 1, wordBreak: "break-all", whiteSpace: "pre-wrap" }}>
            {secret}
          </Code>
          <CopyButton value={secret} timeout={2500}>
            {({ copied, copy }) => (
              <Button variant="default" onClick={copy} leftSection={copied ? <IconCheck size={16} aria-hidden="true" /> : <IconCopy size={16} aria-hidden="true" />}>
                {copied ? "Copied" : "Copy"}
              </Button>
            )}
          </CopyButton>
        </Group>
        {children}
        <Group justify="flex-end">
          <Button onClick={onClose}>I have saved it</Button>
        </Group>
      </Stack>
    </Modal>
  );
}
