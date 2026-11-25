import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { DailyReportEmail } from "./Admin";

afterEach(() => vi.restoreAllMocks());

const data = {
  items: [
    {
      id: "s1",
      name: "Ops daily",
      report: "daily_summary",
      enabled: true,
      recipients: ["a@example.org", "b@example.org", "c@example.org"],
      sender_identity_id: "",
      sender_email: "",
      project_id: "",
      project: "",
      send_hour: 6,
      next_send_at: "2026-11-27T06:00:00Z",
      last_sent_at: null,
      last_sent_period: null,
      last_error: "",
    },
  ],
  reports: [
    { id: "daily_summary", name: "Daily summary", description: "Yesterday.", cadence: "daily" },
    { id: "weekly_summary", name: "Weekly summary", description: "Last week.", cadence: "weekly" },
  ],
  senders: [],
  projects: [{ id: "p1", name: "sacas" }],
  effective_sender: "ops@notify.example.com",
  limit: 50,
};

function wrap() {
  return render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <DailyReportEmail />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("lists the available reports and the schedules", async () => {
  vi.spyOn(admin, "get").mockResolvedValue(data);
  wrap();
  expect(await screen.findByText("Ops daily")).toBeTruthy();
  expect(screen.getByText("Weekly summary")).toBeTruthy();
  expect(screen.getByText("a@example.org, b@example.org +1")).toBeTruthy();
  expect(screen.getByText(/All projects · 06:00 UTC/)).toBeTruthy();
});

it("adds a weekly schedule for one project from the catalogue", async () => {
  vi.spyOn(admin, "get").mockResolvedValue(data);
  const send = vi.spyOn(admin, "send").mockResolvedValue({ id: "s2" });
  wrap();
  await screen.findByText("Ops daily");
  const weekly = screen.getByText("Weekly summary").closest("li")!;
  fireEvent.click(within(weekly).getByRole("button", { name: /Schedule this report/ }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.change(within(dialog).getByPlaceholderText("e.g. Operations team"), {
    target: { value: "Sacas weekly" },
  });
  fireEvent.change(within(dialog).getByPlaceholderText("ops@example.com"), {
    target: { value: "lead@example.org, boss@example.org" },
  });
  const [project, hour] = within(dialog).getAllByRole("combobox").slice(1, 3);
  fireEvent.change(project, { target: { value: "p1" } });
  fireEvent.change(hour, { target: { value: "8" } });
  expect(within(dialog).getByText("On Mondays.")).toBeTruthy();
  fireEvent.click(within(dialog).getByRole("button", { name: "Add schedule" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/report-schedules", "POST", {
      name: "Sacas weekly",
      report: "weekly_summary",
      enabled: true,
      recipients: ["lead@example.org", "boss@example.org"],
      sender_identity_id: "",
      project_id: "p1",
      send_hour: 8,
    }),
  );
});

it("pauses a schedule without losing its settings", async () => {
  vi.spyOn(admin, "get").mockResolvedValue(data);
  const send = vi.spyOn(admin, "send").mockResolvedValue(undefined);
  wrap();
  await screen.findByText("Ops daily");
  fireEvent.click(screen.getByRole("button", { name: "Pause" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/report-schedules/s1", "PUT", {
      name: "Ops daily",
      report: "daily_summary",
      enabled: false,
      recipients: ["a@example.org", "b@example.org", "c@example.org"],
      sender_identity_id: "",
      project_id: "",
      send_hour: 6,
    }),
  );
});
