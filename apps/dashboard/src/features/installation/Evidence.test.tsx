import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { Checklist } from "@/features/workspace/pages/Overview";
import { DeliveryInfrastructure, type InstallationState } from "./Installation";

afterEach(() => {
  vi.restoreAllMocks();
});

function wrap(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <WorkspaceProvider project="demo">
          <Routes>
            <Route path="*" element={ui} />
          </Routes>
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const installation: InstallationState = {
  welcome_state: "degraded",
  wizard: { selected_mode: "existing_smtp" },
  active_profile: { id: "p1", mode: "existing_smtp", config_revision: 3 },
  profile: { id: "p1", mode: "existing_smtp", config_revision: 3 },
  rejections: [
    {
      problem_code: "SMTP_REJECT_RELAY",
      smtp_status: "554",
      reason: "5.7.1 <address>: Relay access denied",
      count: 4,
      last_at: "2026-11-28T09:00:00Z",
    },
  ],
  readiness: {
    scope: "installation",
    mode: "existing_smtp",
    status: "stale",
    progress: { complete: 2, required: 4 },
    steps: [
      {
        id: "configure",
        title: "Connect your mail server",
        status: "ready",
        required: true,
        confirmed_at: "2026-11-28T08:00:00Z",
      },
      {
        id: "verify_connection",
        title: "Verify connection and sign-in",
        status: "stale",
        required: true,
        problem_code: "CHECK_STALE",
        stale_reason: "settings_changed",
        changed_at: "2026-11-28T07:00:00Z",
      },
      {
        id: "verify_sender",
        title: "Send a test message",
        status: "stale",
        required: true,
        problem_code: "CHECK_STALE",
        stale_reason: "expired",
        checked_at: "2026-11-26T07:00:00Z",
      },
    ],
  },
};

it("says why each step is stale, offers the right action and lists refusals", async () => {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) =>
    path === "/installation" ? installation : {},
  );
  const send = vi.spyOn(admin, "send").mockResolvedValue({ ok: true } as never);
  wrap(<DeliveryInfrastructure />);
  await screen.findByText(/The settings changed after this check/);
  const step = (id: string) => document.querySelector(`[data-step="${id}"]`) as HTMLElement;
  expect(
    within(step("configure")).getByText(/Confirmed by live mail handed over at/),
  ).toBeInTheDocument();
  expect(
    within(step("verify_sender")).getByText(/no recent live mail confirms it/),
  ).toBeInTheDocument();
  expect(
    within(step("verify_sender")).getByRole("link", { name: "Send a test message" }),
  ).toBeInTheDocument();
  expect(within(step("verify_sender")).queryByRole("button")).not.toBeInTheDocument();
  expect(
    within(screen.getByTestId("rejections")).getByText(/Relay access denied/),
  ).toBeInTheDocument();
  expect(within(screen.getByTestId("rejections")).getByText(/will not relay/)).toBeInTheDocument();
  fireEvent.click(
    within(step("verify_connection")).getByRole("button", { name: "Run checks again" }),
  );
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/installation/transport-profiles/p1/test", "POST"),
  );
});

async function banner(status: string, handedOver: string | null) {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path.endsWith("/checklist"))
      return {
        scope: "project",
        complete: false,
        progress: { complete: 0, required: 0 },
        steps: [],
        installation: {
          scope: "installation",
          status,
          progress: { complete: 2, required: 4 },
          welcome_state: "degraded",
          live_handover_at: handedOver,
        },
      };
    return {};
  });
  wrap(<Checklist />);
  await waitFor(() =>
    expect(document.querySelector(`[data-installation-status="${status}"]`)).not.toBeNull(),
  );
  return document.querySelector(`[data-installation-status="${status}"]`) as HTMLElement;
}

it("does not warn that live mail may not go out when it just did", async () => {
  const el = await banner("stale", "2026-11-28T08:00:00Z");
  expect(el).toHaveTextContent("recently accepted live mail");
  expect(el).not.toHaveTextContent("may not go out");
});

it("keeps the warning when nothing shows live mail going out", async () => {
  expect(await banner("stale", null)).toHaveTextContent("may not go out");
});
