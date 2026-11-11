import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { MailServer } from "@/features/workspace/pages/Admin";
import { Checklist } from "@/features/workspace/pages/Overview";
import {
  DeliveryInfrastructure,
  DeliveryModeCard,
  ReadinessSteps,
  type InstallationState,
  type Readiness,
} from "./Installation";

afterEach(() => {
  vi.restoreAllMocks();
});

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <WorkspaceProvider project="demo">{ui}</WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const smtpReadiness: Readiness = {
  scope: "installation",
  mode: "existing_smtp",
  status: "stale",
  progress: { complete: 2, required: 4 },
  steps: [
    { id: "choose_mode", title: "Choose how mail is sent", status: "ready", required: true },
    { id: "configure", title: "Connect your mail server", status: "ready", required: true },
    {
      id: "verify_connection",
      title: "Verify connection and sign-in",
      status: "stale",
      required: true,
      problem_code: "CHECK_STALE",
      checked_at: "2026-11-11T10:00:00Z",
    },
    {
      id: "verify_sender",
      title: "Send a test message",
      status: "needs_action",
      required: true,
      problem_code: "SMTP_AUTH_FAILED",
    },
  ],
};

function state(over: Partial<InstallationState>): InstallationState {
  return {
    readiness: smtpReadiness,
    welcome_state: "in_progress",
    wizard: { selected_mode: "existing_smtp" },
    active_profile: { id: "p1", mode: "existing_smtp", config_revision: 3 },
    profile: { id: "p1", mode: "existing_smtp", config_revision: 3 },
    ...over,
  };
}

function mockAdmin(inst: InstallationState, mailServer: Record<string, unknown> = {}) {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path === "/installation") return inst;
    if (path === "/mail-server")
      return {
        smtp_host: "mail.example.com",
        smtp_port: 587,
        smtp_tls_mode: "starttls",
        provider_type: "generic",
        ...mailServer,
      };
    if (path === "/mail-server/status") return {};
    return {};
  });
}

it("offers exactly the two delivery modes", () => {
  const onChange = vi.fn();
  wrap(<DeliveryModeCard value="" onChange={onChange} />);
  const radios = screen.getAllByRole("radio");
  expect(radios.map((r) => (r as HTMLInputElement).value)).toEqual([
    "managed_mta",
    "existing_smtp",
  ]);
  fireEvent.click(screen.getByLabelText(/Connect an existing mail server/));
  expect(onChange).toHaveBeenCalledWith("existing_smtp");
});

it("shows backend progress, plain status text and the fix for each problem", () => {
  wrap(<ReadinessSteps readiness={smtpReadiness} />);
  expect(screen.getByText(/2\/4/)).toBeInTheDocument();
  const bar = screen.getByRole("progressbar");
  expect(bar).toHaveAttribute("aria-valuemax", "4");
  const stale = document.querySelector('[data-step="verify_connection"]') as HTMLElement;
  expect(within(stale).getByText(/Out of date/)).toBeInTheDocument();
  expect(within(stale).getByText(/Run the check again/)).toBeInTheDocument();
  const auth = document.querySelector('[data-step="verify_sender"]') as HTMLElement;
  expect(within(auth).getByText(/login was rejected/)).toBeInTheDocument();
  expect(within(auth).getByText("SMTP_AUTH_FAILED")).toBeInTheDocument();
});

it("persists the mode choice through the wizard API and only after Continue", async () => {
  mockAdmin(
    state({
      welcome_state: "ready_to_configure",
      wizard: {},
      active_profile: null,
      profile: null,
      readiness: {
        scope: "installation",
        status: "not_started",
        progress: { complete: 0, required: 1 },
        steps: [],
      },
    }),
  );
  const send = vi
    .spyOn(admin, "send")
    .mockResolvedValue(state({ wizard: { selected_mode: "managed_mta" } }) as never);
  wrap(<DeliveryInfrastructure />);
  fireEvent.click(await screen.findByLabelText(/Send from my VPS/));
  expect(send).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/installation/wizard", "PUT", {
      selected_mode: "managed_mta",
    }),
  );
});

it("does not offer a mode change while a transport is active", async () => {
  mockAdmin(state({}));
  wrap(<DeliveryInfrastructure />);
  expect(await screen.findByText(/settings revision/)).toBeInTheDocument();
  expect(screen.queryAllByRole("radio")).toHaveLength(0);
});

it("offers to continue an existing installation found from an older version", async () => {
  mockAdmin(state({ welcome_state: "legacy_detected", wizard: {} }));
  const send = vi.spyOn(admin, "send").mockResolvedValue(state({}) as never);
  wrap(<DeliveryInfrastructure />);
  fireEvent.click(await screen.findByRole("button", { name: "Continue existing installation" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/installation/wizard", "PUT", {
      selected_mode: "existing_smtp",
    }),
  );
});

it("generic SMTP screen has no Mailcow requirement; Mailcow fields appear only when chosen", async () => {
  mockAdmin(state({}));
  wrap(<MailServer />);
  expect(await screen.findByLabelText(/^SMTP host/)).toBeInTheDocument();
  await waitFor(() => expect(screen.getByLabelText(/^Mail server type/)).toHaveValue("generic"));
  expect(screen.queryByText(/Mailcow API/)).not.toBeInTheDocument();
  expect(screen.queryByLabelText(/^API key/)).not.toBeInTheDocument();
  expect(screen.queryByText(/Connection to Mailcow/)).not.toBeInTheDocument();
  fireEvent.change(screen.getByLabelText(/^Mail server type/), { target: { value: "mailcow" } });
  expect(screen.getByText("Mailcow API (optional)")).toBeInTheDocument();
  expect(screen.getByLabelText(/^API key/)).toBeInTheDocument();
});

it("managed mode never shows the SMTP password form", async () => {
  mockAdmin(
    state({
      active_profile: null,
      wizard: { selected_mode: "managed_mta" },
      profile: { id: "m1", mode: "managed_mta", config_revision: 1 },
      readiness: {
        scope: "installation",
        mode: "managed_mta",
        status: "needs_action",
        progress: { complete: 1, required: 4 },
        steps: [
          { id: "choose_mode", title: "Choose how mail is sent", status: "ready", required: true },
          {
            id: "install",
            title: "Install the mail delivery service",
            status: "not_started",
            required: true,
            problem_code: "INSTALLER_NOT_AVAILABLE",
          },
        ],
      },
    }),
  );
  wrap(<MailServer />);
  expect(await screen.findByText(/host installer/)).toBeInTheDocument();
  expect(screen.getByText("njiwa-mail preflight -record")).toBeInTheDocument();
  // Wait for the mail-server settings to load too, so a form that renders
  // late would still be caught.
  await waitFor(() => expect(admin.get).toHaveBeenCalledWith("/mail-server"));
  expect(screen.queryByLabelText(/^SMTP host/)).not.toBeInTheDocument();
  expect(screen.queryByLabelText(/^Password/)).not.toBeInTheDocument();
  expect(screen.queryByLabelText(/^Mail server type/)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Test connection" })).not.toBeInTheDocument();
});

it("project checklist uses the backend denominator and keeps installation separate", async () => {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path.endsWith("/checklist"))
      return {
        scope: "project",
        complete: false,
        progress: { complete: 1, required: 5 },
        steps: [
          { key: "domain", title: "Add a domain", done: true, required: true },
          { key: "dns", title: "Publish DNS records", done: false, required: true },
          {
            key: "mailbox",
            title: "Create a mailbox",
            done: false,
            required: false,
            optional: true,
          },
        ],
        installation: {
          scope: "installation",
          status: "stale",
          progress: { complete: 2, required: 4 },
          welcome_state: "degraded",
        },
      };
    return {};
  });
  wrap(<Checklist />);
  expect(await screen.findByTestId("project-progress")).toHaveTextContent("1/5");
  expect(screen.queryByText("Connect mail server")).not.toBeInTheDocument();
  const banner = document.querySelector('[data-installation-status="stale"]') as HTMLElement;
  expect(within(banner).getByText(/Out of date/)).toBeInTheDocument();
  expect(screen.getByText(/\(optional\)/)).toBeInTheDocument();
});

it("sends a controlled test through the queue of the current project", async () => {
  mockAdmin(state({}));
  const send = vi.spyOn(admin, "send").mockResolvedValue({ id: "m-1", status: "queued" } as never);
  wrap(<MailServer />);
  fireEvent.change(await screen.findByLabelText("Send test to"), {
    target: { value: "you@example.org" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send through the queue" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/transport-test", "POST", {
      to: "you@example.org",
    }),
  );
  expect(await screen.findByRole("link", { name: "view the message" })).toHaveAttribute(
    "href",
    "/p/demo/messages/m-1",
  );
});
