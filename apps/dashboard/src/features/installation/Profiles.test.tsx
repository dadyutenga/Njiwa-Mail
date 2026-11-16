import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { DeliveryHealthCard, TransportProfiles } from "./Profiles";

afterEach(() => vi.restoreAllMocks());

function wrap(ui: ReactNode) {
  return render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">{ui}</WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

const profiles = {
  profiles: [
    {
      id: "a",
      mode: "existing_smtp",
      label: "Old SMTP",
      provider_type: "generic",
      config_revision: 2,
      status: "ready",
      is_active: true,
      config: { host: "old.example", port: 587 },
    },
    {
      id: "b",
      mode: "existing_smtp",
      label: "New SMTP",
      provider_type: "generic",
      config_revision: 1,
      status: "ready",
      is_active: false,
      config: { host: "new.example", port: 465 },
    },
  ],
};

function mock(plan: Record<string, unknown>) {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path === "/installation/transport-profiles") return profiles;
    if (path === "/installation/transport-profiles/b/activation-plan")
      return {
        target_id: "b",
        target_mode: "existing_smtp",
        already_active: false,
        queued: 3,
        scheduled: 1,
        submitting: 0,
        retry_wait: 2,
        unknown: 0,
        verified: true,
        blockers: [],
        needs_decision: true,
        ...plan,
      };
    if (path === "/installation")
      return {
        readiness: {
          mode: "existing_smtp",
          status: "ready",
          progress: { complete: 4, required: 4 },
          steps: [],
        },
        previous_profile_id: "",
      };
    if (path === "/mail-server/status")
      return { queue_depth: 7, retry_wait: 2, unknown: 1, last_submitted_at: null };
    return {};
  });
}

it("needs an explicit acknowledgement before moving pending mail to another profile", async () => {
  mock({});
  const send = vi.spyOn(admin, "send").mockResolvedValue({} as never);
  wrap(<TransportProfiles />);
  fireEvent.click(await screen.findByRole("button", { name: "Review switch" }));
  const review = await screen.findByTestId("switch-review");
  expect(within(review).getByText("Queued").nextSibling).toHaveTextContent("3");
  const go = within(review).getByRole("button", { name: "Switch to this profile" });
  expect(go).toBeDisabled();
  fireEvent.click(within(review).getByRole("checkbox"));
  expect(within(review).getByText(/Send the/)).toHaveTextContent("6");
  fireEvent.click(go);
  fireEvent.click(await screen.findByRole("button", { name: "Switch" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/installation/transport-profiles/b/activate", "POST", {
      migrate_pending: true,
    }),
  );
});

it("explains blockers and offers no switch while they remain", async () => {
  mock({
    blockers: ["profile_not_verified", "unknown_jobs_require_reconciliation"],
    unknown: 2,
    needs_decision: false,
  });
  wrap(<TransportProfiles />);
  fireEvent.click(await screen.findByRole("button", { name: "Review switch" }));
  const review = await screen.findByTestId("switch-review");
  expect(
    within(review).getByText(/Run the connection test for this profile first/),
  ).toBeInTheDocument();
  expect(within(review).getByText(/never moved to another server/)).toBeInTheDocument();
  expect(
    within(review).queryByRole("button", { name: "Switch to this profile" }),
  ).not.toBeInTheDocument();
});

it("shows delivery health on the dashboard", async () => {
  mock({});
  wrap(<DeliveryHealthCard />);
  const card = await screen.findByTestId("delivery-health");
  await waitFor(() =>
    expect(within(card).getByText("In the queue").nextSibling).toHaveTextContent("7"),
  );
  expect(within(card).getByText("Unknown outcome").nextSibling).toHaveTextContent("1");
  expect(within(card).getByText("Existing mail server")).toBeInTheDocument();
});

it("edits a profile in place and keeps the stored password when the field is left empty", async () => {
  mock({});
  const send = vi.spyOn(admin, "send").mockResolvedValue({} as never);
  wrap(<TransportProfiles />);
  const row = (await screen.findByText("New SMTP")).closest("li") as HTMLElement;
  fireEvent.click(within(row).getByRole("button", { name: "Edit" }));
  const host = within(row).getByLabelText("SMTP host");
  expect(host).toHaveValue("new.example");
  fireEvent.change(host, { target: { value: "smtp2.example" } });
  fireEvent.click(within(row).getByRole("button", { name: "Save profile" }));
  await waitFor(() => expect(send).toHaveBeenCalled());
  const [path, method, body] = send.mock.calls[0] as [
    string,
    string,
    { smtp: Record<string, unknown> },
  ];
  expect(path).toBe("/installation/transport-profiles/b");
  expect(method).toBe("PUT");
  expect(body.smtp.host).toBe("smtp2.example");
  expect("password" in body.smtp).toBe(false);
});

it("shows the mail host's Postfix and signer health to operators in managed mode", async () => {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path === "/installation")
      return {
        readiness: {
          mode: "managed_mta",
          status: "ready",
          progress: { complete: 1, required: 1 },
          steps: [],
        },
      };
    if (path === "/mta/status")
      return {
        configured: true,
        reachable: true,
        status: { queue: { deferred: 3, hold: 1 }, postfix_active: true, signer_active: false },
      };
    return {};
  });
  wrap(<DeliveryHealthCard />);
  const line = await screen.findByTestId("host-health");
  expect(line).toHaveTextContent("Postfix running");
  expect(line).toHaveTextContent("DKIM signer stopped");
  expect(line).toHaveTextContent("3 deferred");
});
