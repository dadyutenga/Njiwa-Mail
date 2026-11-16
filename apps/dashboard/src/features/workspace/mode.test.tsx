import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Link, MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "@/features/auth/auth";
import { Sandbox } from "./pages/Mail";
import { admin } from "@/lib/admin-api";
import { LegacyWorkspaceRoute } from "./Layout";
import { lastWorkspace, rememberWorkspace, useWorkspace, WorkspaceProvider } from "./context";
import { SendingMode, useSendingModeParam } from "./SendingMode";
import { ComposePanel } from "./pages/Compose";
import { AppResolver } from "./pages/Overview";
import { ApiKeys } from "./pages/Developers";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function WorkspaceState() {
  const { api, to } = useWorkspace();
  const location = useLocation();
  return (
    <div data-testid="workspace-state">
      {api.base}|{to("dashboard")}|{location.pathname}
      {location.search}
    </div>
  );
}

function setupApi() {
  vi.spyOn(admin, "get").mockImplementation(async (path) => {
    if (path.includes("/senders"))
      return {
        items: [
          { id: "sender", email: "sender@example.com", status: "verified", is_default: true },
        ],
      };
    if (path.endsWith("/context")) return { environment_enabled: true };
    return { items: [] };
  });
}

it("ignores a saved global Sandbox mode and remembers only the project", () => {
  localStorage.setItem("njiwa.workspace", JSON.stringify({ project: "demo", env: "sandbox" }));
  expect(lastWorkspace()).toEqual({ project: "demo" });
  rememberWorkspace("other");
  expect(JSON.parse(localStorage.getItem("njiwa.workspace")!)).toEqual({ project: "other" });
});

it("opens a remembered project in the main workspace despite a legacy Sandbox preference", async () => {
  vi.spyOn(admin, "me").mockResolvedValue({
    id: "user",
    email: "test@example.com",
    role: "admin",
    csrf_token: "csrf",
    installation_operator: true,
    mfa_enabled: false,
    mfa_enrollment_required: false,
    expires_at: "2030-01-01T00:00:00Z",
  });
  localStorage.setItem("njiwa.workspace", JSON.stringify({ project: "demo", env: "sandbox" }));
  vi.spyOn(admin, "get").mockResolvedValue({
    items: [{ id: "project", name: "demo" }],
    has_more: false,
  });
  render(
    <MemoryRouter initialEntries={["/app"]}>
      <QueryClientProvider client={new QueryClient()}>
        <AuthProvider>
          <Routes>
            <Route path="/app" element={<AppResolver />} />
            <Route path="/p/demo/dashboard" element={<div>Main Live workspace</div>} />
          </Routes>
        </AuthProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("Main Live workspace")).toBeInTheDocument();
});

it("creates a Sandbox API key using the local key-page choice", async () => {
  setupApi();
  vi.spyOn(admin, "send").mockResolvedValue({ key: "sandbox-key" });
  render(
    <MemoryRouter initialEntries={["/p/demo/keys"]}>
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceProvider project="demo">
          <WorkspaceState />
          <ApiKeys />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  fireEvent.change(screen.getByRole("combobox", { name: "Sending mode" }), {
    target: { value: "sandbox" },
  });
  fireEvent.click(screen.getAllByRole("button", { name: "Create API key" })[0]);
  fireEvent.submit(
    await screen.findByRole("textbox", { name: /Label/ }).then((input) => input.closest("form")!),
  );
  await waitFor(() =>
    expect(admin.send).toHaveBeenCalledWith("/w/demo/sandbox/keys", "POST", expect.anything()),
  );
  expect(screen.getByTestId("workspace-state")).toHaveTextContent(
    "/w/demo/live|/p/demo/dashboard|/p/demo/keys?mode=sandbox",
  );
});

it("keeps the workspace Live when a sending page selects Sandbox", async () => {
  setupApi();
  function SendingPage() {
    const [mode, setMode] = useSendingModeParam();
    return (
      <>
        <SendingMode value={mode} onChange={setMode} />
        <Link to="/p/demo/dashboard">Dashboard</Link>
      </>
    );
  }
  render(
    <MemoryRouter initialEntries={["/p/demo/bulk"]}>
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceProvider project="demo">
          <WorkspaceState />
          <Routes>
            <Route path="/p/demo/bulk" element={<SendingPage />} />
            <Route path="/p/demo/dashboard" element={<div>Live dashboard</div>} />
          </Routes>
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  fireEvent.change(screen.getByRole("combobox", { name: "Sending mode" }), {
    target: { value: "sandbox" },
  });
  expect(screen.getByTestId("workspace-state")).toHaveTextContent(
    "/w/demo/live|/p/demo/dashboard|/p/demo/bulk?mode=sandbox",
  );
  fireEvent.click(screen.getByRole("link", { name: "Dashboard" }));
  expect(await screen.findByText("Live dashboard")).toBeInTheDocument();
  expect(screen.getByTestId("workspace-state")).toHaveTextContent(
    "/w/demo/live|/p/demo/dashboard|/p/demo/dashboard",
  );
});

it("shows Sandbox captures inside the Live workspace", async () => {
  setupApi();
  render(
    <MemoryRouter initialEntries={["/p/demo/captures"]}>
      <QueryClientProvider client={new QueryClient()}>
        <WorkspaceProvider project="demo">
          <WorkspaceState />
          <Sandbox />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  expect(await screen.findByText("No captured mail")).toBeInTheDocument();
  expect(admin.get).toHaveBeenCalledWith("/w/demo/sandbox/sandbox?limit=50&offset=0");
  expect(screen.getByTestId("workspace-state")).toHaveTextContent(
    "/w/demo/live|/p/demo/dashboard|/p/demo/captures",
  );
});

it.each([
  ["/p/demo/sandbox/dashboard", "sandbox", "/p/demo/dashboard"],
  ["/p/demo/live/settings", "live", "/p/demo/settings"],
  [
    "/p/demo/sandbox/messages/id?state=captured",
    "sandbox",
    "/p/demo/messages/id?state=captured&mode=sandbox",
  ],
  ["/p/demo/sandbox/sandbox/id", "sandbox", "/p/demo/captures/id"],
] as const)("redirects legacy workspace URL %s", async (path, mode, destination) => {
  function Destination() {
    const location = useLocation();
    return (
      <div>
        {location.pathname}
        {location.search}
      </div>
    );
  }
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path={`/p/:project/${mode}/*`} element={<LegacyWorkspaceRoute mode={mode} />} />
        <Route path="*" element={<Destination />} />
      </Routes>
    </MemoryRouter>,
  );
  expect(await screen.findByText(destination)).toBeInTheDocument();
});

it.each(["live", "sandbox"] as const)(
  "sends Compose in %s without changing the workspace",
  async (mode) => {
    setupApi();
    vi.spyOn(admin, "send").mockResolvedValue({ blocked: [] });
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ id: "message", status: "queued" }), { status: 200 }),
      );
    vi.stubGlobal("fetch", fetchMock);
    render(
      <MemoryRouter initialEntries={["/p/demo/dashboard"]}>
        <QueryClientProvider client={new QueryClient()}>
          <WorkspaceProvider project="demo">
            <WorkspaceState />
            <ComposePanel
              open
              onOpenChange={vi.fn()}
              prefill={{
                to: "recipient@example.com",
                subject: "Local mode test",
                html: "<p>Hello</p>",
              }}
            />
          </WorkspaceProvider>
        </QueryClientProvider>
      </MemoryRouter>,
    );
    expect(screen.getByRole("combobox", { name: "Sending mode" })).toHaveValue("live");
    if (mode === "sandbox")
      fireEvent.change(screen.getByRole("combobox", { name: "Sending mode" }), {
        target: { value: "sandbox" },
      });
    await waitFor(() =>
      expect(screen.getByRole("combobox", { name: "From" })).toHaveValue("sender@example.com"),
    );
    expect(screen.getByDisplayValue("Local mode test")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: mode === "live" ? "Send" : "Send to sandbox",
      }),
    );
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        `/admin/v1/w/demo/${mode}/compose`,
        expect.objectContaining({ method: "POST" }),
      ),
    );
    expect(admin.send).toHaveBeenCalledWith(
      `/w/demo/${mode}/recipients/check`,
      "POST",
      expect.anything(),
    );
    expect(screen.getByTestId("workspace-state")).toHaveTextContent(
      "/w/demo/live|/p/demo/dashboard|/p/demo/dashboard",
    );
  },
);
