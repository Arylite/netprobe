import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { admin, mockCentral, quietCentral, renderApp, signedInAs, viewer } from "./test/harness";

const future = () => new Date(Date.now() + 3600_000).toISOString();
const ago = (seconds: number) => new Date(Date.now() - seconds * 1000).toISOString();

describe("signing in", () => {
  it("sends a visitor to the login page", async () => {
    renderApp(mockCentral({}), "/edges");
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  it("signs in, opens the overview and sends the token with what follows", async () => {
    const central = mockCentral({
      ...quietCentral,
      "POST /api/v1/login": { body: { token: "tok", expires_at: future(), user: admin } },
    });
    renderApp(central, "/login");
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Username"), "alice");
    await user.type(screen.getByLabelText("Password"), "a long admin password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByRole("heading", { name: "Overview" })).toBeInTheDocument();
    expect(central.called("POST /api/v1/login")[0]?.body).toEqual({ username: "alice", password: "a long admin password" });
    await waitFor(() => expect(central.called("GET /api/v1/status")[0]?.headers.get("Authorization")).toBe("Bearer tok"));
    expect(central.called("POST /api/v1/login")[0]?.headers.get("Authorization")).toBeNull();
  });

  it("says why it was refused and clears the password", async () => {
    const central = mockCentral({ "POST /api/v1/login": { status: 401, body: { error: "invalid username or password" } } });
    renderApp(central, "/login");
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Username"), "alice");
    await user.type(screen.getByLabelText("Password"), "wrong");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("Invalid username or password")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toHaveValue("");
    expect(screen.queryByRole("heading", { name: "Overview" })).not.toBeInTheDocument();
  });

  it("asks for both fields before it calls the central", async () => {
    const central = mockCentral({});
    renderApp(central, "/login");
    await userEvent.setup().click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("Enter your username")).toBeInTheDocument();
    expect(central.calls).toHaveLength(0);
  });

  it("goes back to the page that was asked for", async () => {
    const central = mockCentral({
      ...quietCentral,
      "POST /api/v1/login": { body: { token: "tok", expires_at: future(), user: admin } },
    });
    renderApp(central, "/checks");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Username"), "alice");
    await user.type(screen.getByLabelText("Password"), "a long admin password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("heading", { name: "Checks" })).toBeInTheDocument();
  });

  it("ends the session when the central stops accepting the token", async () => {
    signedInAs(admin);
    renderApp(mockCentral({ ...quietCentral, "GET /api/v1/status": { status: 401, body: { error: "unauthorized" } } }));
    expect(await screen.findByText("Your session has ended. Sign in again.")).toBeInTheDocument();
    expect(sessionStorage.getItem("netprobe.session")).toBeNull();
  });

  it("signs out", async () => {
    signedInAs(admin);
    const central = mockCentral({ ...quietCentral, "POST /api/v1/logout": { status: 204 } });
    renderApp(central);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /alice/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));
    expect(await screen.findByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(central.called("POST /api/v1/logout")).toHaveLength(1);
    expect(sessionStorage.getItem("netprobe.session")).toBeNull();
  });
});

describe("what each role sees", () => {
  it("keeps the administration away from a viewer", async () => {
    signedInAs(viewer);
    renderApp(mockCentral(quietCentral), "/users");
    expect(await screen.findByText("This page is for administrators.")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Users" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Channels" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Edges" })).toBeInTheDocument();
  });

  it("gives an administrator the whole menu", async () => {
    signedInAs(admin);
    renderApp(mockCentral(quietCentral));
    expect(await screen.findByRole("link", { name: "Users" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Channels" })).toBeInTheDocument();
  });

  it("shows a viewer the edges without the means to change them", async () => {
    signedInAs(viewer);
    const edges = [{ id: "e1", name: "paris", created_at: ago(86400), last_seen: ago(10) }];
    renderApp(mockCentral({ ...quietCentral, "GET /api/v1/edges": { body: { edges } } }), "/edges");
    expect(await screen.findByText("paris")).toBeInTheDocument();
    expect(screen.getByText("Reporting")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add an edge" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Revoke/ })).not.toBeInTheDocument();
  });
});

describe("the overview", () => {
  it("tells what works, what fails and what went quiet, in words", async () => {
    signedInAs(viewer);
    const row = (edge: string, ok: boolean, last: number) => ({
      edge_id: edge,
      edge,
      last_at: ago(last),
      ok,
      rtt_millis: 12,
      error: ok ? undefined : "connection refused",
      samples: 100,
      success_ratio: ok ? 1 : 0.5,
      p95_rtt_millis: 20,
    });
    renderApp(
      mockCentral({
        ...quietCentral,
        "GET /api/v1/status": {
          body: {
            window_seconds: 86400,
            checks: [
              { id: "web", kind: "http", target: "https://example.com", interval_seconds: 30, edges: [row("paris", true, 10), row("lyon", false, 10), row("oslo", true, 3600)] },
              { id: "db", kind: "tcp", target: "db:5432", interval_seconds: 30, edges: [] },
            ],
          },
        },
        "GET /api/v1/incidents": {
          body: { incidents: [{ id: 1, kind: "check", check_id: "web", edge_id: "lyon", edge: "lyon", started_at: ago(120), detail: "3 failures in a row" }] },
        },
        "GET /api/v1/edges": { body: { edges: [{ id: "a", name: "paris", created_at: ago(9999) }, { id: "b", name: "gone", created_at: ago(9999), revoked_at: ago(10) }] } },
      }),
    );

    expect(await screen.findByText("OK")).toBeInTheDocument();
    expect(screen.getByText("Failing")).toBeInTheDocument();
    expect(screen.getByText("No recent run")).toBeInTheDocument();
    expect(screen.getByText("No result in the last 24 hours")).toBeInTheDocument();
    expect(screen.getByText("connection refused")).toBeInTheDocument();
    expect(screen.getByText("1 open incident")).toBeInTheDocument();
    expect(screen.getByText("web on lyon")).toBeInTheDocument();

    const failing = screen.getByText("Checks failing now").parentElement;
    expect(failing).toHaveTextContent("1");
    expect(failing).toHaveTextContent("of 2");
    expect(screen.getByText("Active edges").parentElement).toHaveTextContent("1");
  });

  it("explains an empty installation", async () => {
    signedInAs(viewer);
    renderApp(mockCentral(quietCentral));
    expect(await screen.findByText("No check yet. Add one in Checks.")).toBeInTheDocument();
  });

  it("says so when the central cannot answer, and offers to try again", async () => {
    signedInAs(viewer);
    const central = mockCentral({ ...quietCentral, "GET /api/v1/status": { status: 503, body: { error: "service unavailable" } } });
    renderApp(central);
    expect(await screen.findByText("Service unavailable")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});

describe("edges", () => {
  it("registers an edge and shows its token once", async () => {
    signedInAs(admin);
    const central = mockCentral({
      ...quietCentral,
      "POST /api/v1/edges": { status: 201, body: { id: "e1", name: "paris", token: "np_the-secret-token" } },
    });
    renderApp(central, "/edges");
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Add an edge" }));
    await user.type(await screen.findByLabelText("Name"), "paris");
    await user.click(screen.getByRole("button", { name: "Add" }));

    const dialog = await screen.findByRole("dialog", { name: "Token of paris" });
    expect(within(dialog).getByText("np_the-secret-token")).toBeInTheDocument();
    expect(within(dialog).getByText("Copy it now: it is not shown again.")).toBeInTheDocument();
    expect(central.called("POST /api/v1/edges")[0]?.body).toEqual({ name: "paris" });

    await user.click(within(dialog).getByRole("button", { name: "I have saved it" }));
    await waitFor(() => expect(screen.queryByText("np_the-secret-token")).not.toBeInTheDocument());
  });

  it("refuses a bad name before it calls the central", async () => {
    signedInAs(admin);
    const central = mockCentral(quietCentral);
    renderApp(central, "/edges");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Add an edge" }));
    await user.type(await screen.findByLabelText("Name"), "Paris Office");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText(/Use 1 to 63 lowercase letters/)).toBeInTheDocument();
    expect(central.called("POST /api/v1/edges")).toHaveLength(0);
  });

  it("shows what the central refuses", async () => {
    signedInAs(admin);
    renderApp(mockCentral({ ...quietCentral, "POST /api/v1/edges": { status: 409, body: { error: "already exists" } } }), "/edges");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Add an edge" }));
    await user.type(await screen.findByLabelText("Name"), "paris");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText("Already exists")).toBeInTheDocument();
  });

  it("asks before it revokes an edge", async () => {
    signedInAs(admin);
    const edges = [{ id: "e1", name: "paris", created_at: ago(86400), last_seen: ago(10) }];
    const central = mockCentral({ ...quietCentral, "GET /api/v1/edges": { body: { edges } }, "DELETE /api/v1/edges/paris": { status: 204 } });
    renderApp(central, "/edges");
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Revoke paris" }));
    expect(central.called("DELETE /api/v1/edges/paris")).toHaveLength(0);
    expect(await screen.findByText("Revoke paris?")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(central.called("DELETE /api/v1/edges/paris")).toHaveLength(1));
  });
});

describe("checks", () => {
  it("adds a check with the interval as a number", async () => {
    signedInAs(admin);
    const central = mockCentral({
      ...quietCentral,
      "POST /api/v1/checks": { status: 201, body: { id: "web", kind: "http", target: "https://example.com", interval_seconds: 60 } },
    });
    renderApp(central, "/checks");
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Add a check" }));
    await user.type(await screen.findByLabelText("Name"), "web");
    await user.click(screen.getByRole("combobox", { name: "Kind" }));
    await user.click(await screen.findByRole("option", { name: "HTTP request" }));
    await user.type(screen.getByLabelText("Target"), "https://example.com");
    await user.clear(screen.getByLabelText("Run every (seconds)"));
    await user.type(screen.getByLabelText("Run every (seconds)"), "60");
    await user.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => expect(central.called("POST /api/v1/checks")).toHaveLength(1));
    expect(central.called("POST /api/v1/checks")[0]?.body).toEqual({ id: "web", kind: "http", target: "https://example.com", interval_seconds: 60 });
  });

  it("lists the results of a check with the name of each edge", async () => {
    signedInAs(viewer);
    renderApp(
      mockCentral({
        ...quietCentral,
        "GET /api/v1/edges": { body: { edges: [{ id: "e1", name: "paris", created_at: ago(9999) }] } },
        "GET /api/v1/checks/web/results": {
          body: { results: [{ edge_id: "e1", at: ago(5), ok: false, rtt_millis: 3, error: "connection refused" }, { edge_id: "e1", at: ago(35), ok: true, rtt_millis: 1250 }] },
        },
      }),
      "/checks/web",
    );
    expect(await screen.findByRole("heading", { name: "web" })).toBeInTheDocument();
    expect(await screen.findAllByText("paris")).toHaveLength(2);
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("1.25 s")).toBeInTheDocument();
  });
});

describe("channels", () => {
  it("adds a channel with its secret, tests it and removes it", async () => {
    signedInAs(admin);
    const channels = [{ name: "ops", url: "https://hooks.example.com/x", has_secret: true, created_at: ago(100) }];
    const central = mockCentral({
      ...quietCentral,
      "GET /api/v1/channels": { body: { channels } },
      "POST /api/v1/channels": { status: 201, body: channels[0] },
      "POST /api/v1/channels/ops/test": { status: 204 },
      "DELETE /api/v1/channels/ops": { status: 204 },
    });
    renderApp(central, "/channels");
    const user = userEvent.setup();

    await user.click(await screen.findByRole("button", { name: "Add a channel" }));
    await user.type(await screen.findByLabelText("Name"), "ops");
    await user.type(screen.getByLabelText("Webhook address"), "https://hooks.example.com/x");
    await user.type(screen.getByLabelText(/Signing secret/), "s3cret");
    await user.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(central.called("POST /api/v1/channels")).toHaveLength(1));
    expect(central.called("POST /api/v1/channels")[0]?.body).toEqual({ name: "ops", url: "https://hooks.example.com/x", secret: "s3cret" });

    await user.click(await screen.findByRole("button", { name: "Send a test to ops" }));
    expect(await screen.findByText("ops accepted the test notification")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Remove ops" }));
    await user.click(await screen.findByRole("button", { name: "Remove" }));
    await waitFor(() => expect(central.called("DELETE /api/v1/channels/ops")).toHaveLength(1));
  });

  it("says why a test failed", async () => {
    signedInAs(admin);
    renderApp(
      mockCentral({
        ...quietCentral,
        "GET /api/v1/channels": { body: { channels: [{ name: "ops", url: "https://hooks.example.com/x", has_secret: false, created_at: ago(100) }] } },
        "POST /api/v1/channels/ops/test": { status: 502, body: { error: "the channel did not accept the test: the channel answered 500 Internal Server Error" } },
      }),
      "/channels",
    );
    await userEvent.setup().click(await screen.findByRole("button", { name: "Send a test to ops" }));
    expect(await screen.findByText(/answered 500 Internal Server Error/)).toBeInTheDocument();
  });

  it("refuses an address with credentials in it", async () => {
    signedInAs(admin);
    const central = mockCentral(quietCentral);
    renderApp(central, "/channels");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Add a channel" }));
    await user.type(await screen.findByLabelText("Name"), "ops");
    await user.type(screen.getByLabelText("Webhook address"), "https://user:pw@hooks.example.com/x");
    await user.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText(/without a user name or a password/)).toBeInTheDocument();
    expect(central.called("POST /api/v1/channels")).toHaveLength(0);
  });
});

describe("users", () => {
  it("does not let an administrator delete themself", async () => {
    signedInAs(admin);
    renderApp(
      mockCentral({ ...quietCentral, "GET /api/v1/users": { body: { users: [{ username: "alice", role: "admin" }, { username: "bob", role: "viewer" }] } } }),
      "/users",
    );
    expect(await screen.findByRole("button", { name: "Delete alice" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Delete bob" })).toBeEnabled();
  });

  it("creates an account", async () => {
    signedInAs(admin);
    const central = mockCentral({ ...quietCentral, "POST /api/v1/users": { status: 201, body: { username: "carol", role: "viewer" } } });
    renderApp(central, "/users");
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Add a user" }));
    await user.type(await screen.findByLabelText("Username"), "carol");
    await user.type(screen.getByLabelText("Password"), "a long enough password");
    await user.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(central.called("POST /api/v1/users")).toHaveLength(1));
    expect(central.called("POST /api/v1/users")[0]?.body).toEqual({ username: "carol", role: "viewer", password: "a long enough password" });
  });
});

describe("the account", () => {
  it("changes the password, then signs in again because every session ended", async () => {
    signedInAs(viewer);
    const central = mockCentral({ ...quietCentral, "POST /api/v1/me/password": { status: 204 } });
    renderApp(central, "/account");
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText("Current password"), "the old password");
    await user.type(screen.getByLabelText("New password"), "a brand new password");
    await user.type(screen.getByLabelText("New password, again"), "a brand new password");
    await user.click(screen.getByRole("button", { name: "Change the password" }));

    expect(await screen.findByText("Your password was changed. Sign in with the new one.")).toBeInTheDocument();
    expect(central.called("POST /api/v1/me/password")[0]?.body).toEqual({ current_password: "the old password", new_password: "a brand new password" });
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
  });

  it("catches a typo in the confirmation", async () => {
    signedInAs(viewer);
    const central = mockCentral(quietCentral);
    renderApp(central, "/account");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Current password"), "the old password");
    await user.type(screen.getByLabelText("New password"), "a brand new password");
    await user.type(screen.getByLabelText("New password, again"), "a brand new passwurd");
    await user.click(screen.getByRole("button", { name: "Change the password" }));
    expect(await screen.findByText("The two passwords differ")).toBeInTheDocument();
    expect(central.called("POST /api/v1/me/password")).toHaveLength(0);
  });

  it("shows the central's refusal when the current password is wrong", async () => {
    signedInAs(viewer);
    renderApp(mockCentral({ ...quietCentral, "POST /api/v1/me/password": { status: 403, body: { error: "the current password is wrong" } } }), "/account");
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText("Current password"), "nope");
    await user.type(screen.getByLabelText("New password"), "a brand new password");
    await user.type(screen.getByLabelText("New password, again"), "a brand new password");
    await user.click(screen.getByRole("button", { name: "Change the password" }));
    expect(await screen.findByText("The current password is wrong")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Account" })).toBeInTheDocument();
  });
});

describe("the theme", () => {
  it("is light first, and the switch moves to dark and back", async () => {
    signedInAs(viewer);
    renderApp(mockCentral(quietCentral));
    const user = userEvent.setup();
    await screen.findByRole("heading", { name: "Overview" });
    expect(document.documentElement).toHaveAttribute("data-mantine-color-scheme", "light");

    await user.click(screen.getByRole("button", { name: "Switch to the dark theme" }));
    await waitFor(() => expect(document.documentElement).toHaveAttribute("data-mantine-color-scheme", "dark"));
    expect(localStorage.getItem("mantine-color-scheme-value")).toBe("dark");

    await user.click(screen.getByRole("button", { name: "Switch to the light theme" }));
    await waitFor(() => expect(document.documentElement).toHaveAttribute("data-mantine-color-scheme", "light"));
  });
});
