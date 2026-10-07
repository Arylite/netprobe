import {
  ActionIcon,
  Alert,
  AppShell,
  Burger,
  Button,
  Group,
  Menu,
  NavLink as ShellLink,
  ScrollArea,
  Text,
  Tooltip,
  useComputedColorScheme,
  useMantineColorScheme,
} from "@mantine/core";
import { useDisclosure } from "@mantine/hooks";
import {
  IconBellRinging,
  IconChecklist,
  IconChevronDown,
  IconHistory,
  IconLayoutDashboard,
  IconLogout,
  IconMoon,
  IconRadar2,
  IconServer,
  IconSun,
  IconUserCircle,
  IconUsers,
  IconWebhook,
} from "@tabler/icons-react";
import type { ReactNode } from "react";
import { Link, Navigate, Outlet, useLocation } from "react-router";

import { useSession } from "../auth/session";
import { pageBackground } from "../theme";

interface Item {
  to: string;
  label: string;
  icon: typeof IconServer;
}

const monitor: Item[] = [
  { to: "/", label: "Overview", icon: IconLayoutDashboard },
  { to: "/incidents", label: "Incidents", icon: IconBellRinging },
];

const configure: Item[] = [
  { to: "/edges", label: "Edges", icon: IconServer },
  { to: "/checks", label: "Checks", icon: IconChecklist },
];

const administer: Item[] = [
  { to: "/channels", label: "Channels", icon: IconWebhook },
  { to: "/users", label: "Users", icon: IconUsers },
  { to: "/audit", label: "Audit log", icon: IconHistory },
];

/** Sends whoever is not signed in to the login page, and back after it. */
export function RequireSession() {
  const { user } = useSession();
  const location = useLocation();
  if (!user) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  return <Outlet />;
}

/** Light is the default; this is how a person asks for the dark theme, and back. */
export function ColorSchemeSwitch() {
  const { setColorScheme } = useMantineColorScheme();
  const scheme = useComputedColorScheme("light");
  const next = scheme === "light" ? "dark" : "light";
  return (
    <Tooltip label={`Switch to the ${next} theme`}>
      <ActionIcon variant="default" size="lg" aria-label={`Switch to the ${next} theme`} onClick={() => setColorScheme(next)}>
        {scheme === "light" ? <IconMoon size={18} aria-hidden="true" /> : <IconSun size={18} aria-hidden="true" />}
      </ActionIcon>
    </Tooltip>
  );
}

function Section({ label, items, onNavigate }: { label: string; items: Item[]; onNavigate: () => void }) {
  const { pathname } = useLocation();
  return (
    <>
      <Text size="xs" fw={600} c="dimmed" tt="uppercase" px="sm" pt="md" pb={4}>
        {label}
      </Text>
      {items.map(({ to, label: text, icon: Icon }) => {
        const active = to === "/" ? pathname === "/" : pathname === to || pathname.startsWith(`${to}/`);
        return (
          <ShellLink
            key={to}
            component={Link}
            to={to}
            label={text}
            leftSection={<Icon size={18} aria-hidden="true" />}
            active={active}
            aria-current={active ? "page" : undefined}
            onClick={onNavigate}
          />
        );
      })}
    </>
  );
}

export function Layout() {
  const { user, logout } = useSession();
  const [opened, { toggle, close }] = useDisclosure(false);
  return (
    <AppShell header={{ height: 56 }} navbar={{ width: 240, breakpoint: "sm", collapsed: { mobile: !opened } }} padding="lg">
      <AppShell.Header>
        <Group h="100%" px="md" justify="space-between" wrap="nowrap">
          <Group gap="sm" wrap="nowrap">
            <Burger opened={opened} onClick={toggle} hiddenFrom="sm" size="sm" aria-label="Toggle the navigation" />
            <IconRadar2 size={26} aria-hidden="true" />
            <Text fw={700} size="lg">
              netprobe
            </Text>
          </Group>
          <Group gap="sm" wrap="nowrap">
            <ColorSchemeSwitch />
            <Menu position="bottom-end" width={200}>
              <Menu.Target>
                <Button variant="default" leftSection={<IconUserCircle size={18} aria-hidden="true" />} rightSection={<IconChevronDown size={14} aria-hidden="true" />}>
                  {user?.username}
                </Button>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>{user?.role === "admin" ? "Administrator" : "Viewer"}</Menu.Label>
                <Menu.Item component={Link} to="/account" leftSection={<IconUserCircle size={16} aria-hidden="true" />}>
                  Account
                </Menu.Item>
                <Menu.Item leftSection={<IconLogout size={16} aria-hidden="true" />} onClick={() => void logout()}>
                  Sign out
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>
      <AppShell.Navbar p="xs">
        <ScrollArea>
          <nav aria-label="Main">
            <Section label="Monitor" items={monitor} onNavigate={close} />
            <Section label="Configure" items={user?.role === "admin" ? [...configure, ...administer] : configure} onNavigate={close} />
          </nav>
        </ScrollArea>
      </AppShell.Navbar>
      <AppShell.Main style={{ background: pageBackground }}>
        <Outlet />
      </AppShell.Main>
    </AppShell>
  );
}

/** Keeps the pages that change things away from someone who may only read. */
export function AdminOnly({ children }: { children: ReactNode }) {
  const { user } = useSession();
  if (user?.role !== "admin") {
    return (
      <Alert color="red" variant="light" role="alert">
        This page is for administrators.
      </Alert>
    );
  }
  return children;
}

export function NotFound() {
  return (
    <Alert color="gray" variant="light" title="Page not found">
      <Button component={Link} to="/" variant="default" mt="xs">
        Back to the overview
      </Button>
    </Alert>
  );
}
