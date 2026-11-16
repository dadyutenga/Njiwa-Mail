import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { DeliveryModeCard } from "./Installation";
import { DiscoveryPanel, SetupJourneyPanel, type Candidate } from "./Setup";

afterEach(() => vi.restoreAllMocks());

function wrap(ui: React.ReactNode) {
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">{ui}</WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const steps = [
  {
    n: 1,
    id: "install",
    title: "Install Njiwa-Mail",
    status: "ready",
    detail: "The API and dashboard are running.",
  },
  {
    n: 2,
    id: "detect",
    title: "Detect",
    status: "needs_action",
    detail: "Run the read-only scan on the server.",
    command: "njiwa-mail discover -record",
    route: "mail-server",
  },
  { n: 3, id: "choose_mode", title: "Choose", status: "needs_action", detail: "Choose a mode." },
];

it("shows the ten-step journey with a scan action on the current step", async () => {
  const get = vi.spyOn(admin, "get").mockResolvedValue({
    project: "demo",
    mode: "",
    next: 2,
    done: 1,
    total: 10,
    steps,
  } as never);
  wrap(<SetupJourneyPanel />);
  expect(await screen.findByText(/Steps 5 to 9 are for project/)).toHaveTextContent("demo");
  expect(get).toHaveBeenCalledWith("/installation/setup?project=demo");
  expect(await screen.findByText(/Step 2 of 10/)).toBeInTheDocument();
  const current = screen.getByText(/2\. Detect what is already on the server/).closest("li")!;
  expect(current).toHaveAttribute("aria-current", "step");
  expect(screen.getByRole("button", { name: "Scan this server" })).toBeInTheDocument();
  expect(screen.getAllByText("Done")).toHaveLength(1);
});

const report = {
  scanned_at: "2026-11-13T09:00:00Z",
  host: { os: "Debian 12", public_ips: ["203.0.113.10"], mem_total_mb: 8000, cpus: 4 },
  ports: [{ port: 25, address: "0.0.0.0", process: "docker-proxy", container: "172.22.1.253:25" }],
  mail_services: [],
  mailcow: { hostname: "mail.example.co.tz", running: true },
  recommendation: {
    mode: "existing_smtp",
    reasons: ["This host already runs a mail service."],
    managed_blockers: [
      {
        code: "MAILCOW_PRESENT",
        detail: "Mailcow runs the mail service on this host.",
        fix: "Use it.",
      },
    ],
    warnings: [],
    candidates: [
      {
        kind: "mailcow",
        label: "Mailcow on this server",
        host: "mail.example.co.tz",
        port: 587,
        tls_mode: "starttls",
        note: "Create a mailbox.",
      },
    ],
  },
};

it("explains why managed mode is blocked and fills in the found server", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({ report, recorded_at: report.scanned_at } as never);
  const use = vi.fn();
  wrap(<DiscoveryPanel onUseCandidate={use} />);
  expect(await screen.findByText("MAILCOW_PRESENT")).toBeInTheDocument();
  expect(screen.getByText("Nothing on this server is changed or replaced.")).toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("Connect an existing mail server");
  fireEvent.click(screen.getByRole("button", { name: "Use these settings" }));
  expect((use.mock.calls[0][0] as Candidate).host).toBe("mail.example.co.tz");
});

it("asks for the scan when there is no report", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({ report: null, recorded_at: null } as never);
  wrap(<DiscoveryPanel />);
  expect(await screen.findByText("go run . discover -record")).toBeInTheDocument();
});

it("marks the recommended mode and a blocked managed mode", () => {
  render(
    <DeliveryModeCard value="" onChange={() => {}} recommended="existing_smtp" managedBlocked />,
  );
  const existing = screen.getByLabelText(/Connect an existing mail server/);
  expect(existing.closest("label")).toHaveTextContent("Recommended for this server");
  expect(screen.getByLabelText(/Send from my VPS/).closest("label")).toHaveTextContent(
    "The installer will refuse and change nothing.",
  );
});

it("runs and records discovery from the setup step without a host CLI", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({
    project: "demo",
    next: 2,
    done: 1,
    total: 10,
    steps,
  } as never);
  const send = vi
    .spyOn(admin, "send")
    .mockResolvedValue({ report, recorded_at: report.scanned_at } as never);
  wrap(<SetupJourneyPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "Scan this server" }));
  await waitFor(() => expect(send).toHaveBeenCalledWith("/installation/discovery", "POST"));
  expect(
    await screen.findByText("Scan recorded. The setup steps have been updated."),
  ).toBeInTheDocument();
});

it("keeps scan errors visible and allows retry", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({ report: null, recorded_at: null } as never);
  const send = vi
    .spyOn(admin, "send")
    .mockRejectedValueOnce(new Error("Host agent unavailable"))
    .mockResolvedValueOnce({ report, recorded_at: report.scanned_at } as never);
  wrap(<DiscoveryPanel />);
  fireEvent.click(await screen.findByRole("button", { name: "Scan this server" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Host agent unavailable");
  fireEvent.click(screen.getByRole("button", { name: "Scan this server" }));
  expect(await screen.findByText("MAILCOW_PRESENT")).toBeInTheDocument();
  expect(send).toHaveBeenCalledTimes(2);
});
