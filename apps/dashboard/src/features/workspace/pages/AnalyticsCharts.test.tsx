import { render, screen } from "@testing-library/react";
import { beforeAll, expect, it, vi } from "vitest";
import { DailyCharts, fillDays } from "./AnalyticsCharts";

beforeAll(() => {
  // jsdom has no ResizeObserver; the charts' responsive container needs one.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

it("fills missing days with zeros and works out the other states", () => {
  const rows = fillDays(
    [
      {
        day: "2026-11-02",
        messages: 10,
        submitted: 7,
        failed: 2,
        hard_bounces: 1,
        soft_bounces: 0,
      },
      { day: "2026-11-04T00:00:00Z", messages: "3", submitted: "3", failed: "0" },
    ],
    new Date("2026-11-01T00:00:00Z"),
    new Date("2026-11-04T00:00:00Z"),
  );
  expect(rows.map((r) => r.day)).toEqual(["2026-11-01", "2026-11-02", "2026-11-03", "2026-11-04"]);
  expect(rows[0]).toMatchObject({ messages: 0, other: 0 });
  expect(rows[1]).toMatchObject({
    messages: 10,
    submitted: 7,
    failed: 2,
    other: 1,
    hard_bounces: 1,
  });
  expect(rows[3]).toMatchObject({ messages: 3, other: 0, soft_bounces: 0 });
});

it("names both charts, their series and the totals for screen readers", () => {
  const rows = fillDays(
    [
      {
        day: "2026-11-02",
        messages: 10,
        submitted: 7,
        failed: 2,
        hard_bounces: 1,
        soft_bounces: 2,
      },
    ],
    new Date("2026-11-01T00:00:00Z"),
    new Date("2026-11-02T00:00:00Z"),
  );
  render(<DailyCharts rows={rows} />);
  expect(
    screen.getByRole("img", { name: "Messages per day: 10 Messages, 7 Submitted, 2 Failed" }),
  ).toBeTruthy();
  expect(
    screen.getByRole("img", { name: "Bounces per day: 1 Hard bounces, 2 Soft bounces" }),
  ).toBeTruthy();
  for (const label of ["Submitted", "Failed", "Other states", "Hard bounces", "Soft bounces"])
    expect(screen.getByText(label)).toBeTruthy();
});
