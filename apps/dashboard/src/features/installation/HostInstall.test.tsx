import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { HostInstall } from "./HostInstall";

afterEach(() => vi.restoreAllMocks());

function open(job: Record<string, unknown> = { state: "idle" }) {
  vi.spyOn(admin, "get").mockResolvedValue(job as never);
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <HostInstall />
    </QueryClientProvider>,
  );
}

it("installs only the plan that was reviewed and approved", async () => {
  const send = vi.spyOn(admin, "send").mockImplementation(async (path: string) => {
    if (path.endsWith("/plan"))
      return { digest: "abc123abc123", summary: "install   packages postfix" } as never;
    return { started: true } as never;
  });
  open();
  fireEvent.click(screen.getByRole("button", { name: "Plan" }));
  expect(await screen.findByTestId("host-plan")).toHaveTextContent("install packages postfix");
  const install = screen.getByRole("button", { name: "Approve and install" });
  expect(install).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(install);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/installation/host/apply", "POST", {
      hostname: "",
      bounce_domain: "",
      digest: "abc123abc123",
    }),
  );
});

it("offers no install on a host that already runs a mail server", async () => {
  vi.spyOn(admin, "send").mockResolvedValue({
    digest: "abc",
    summary: "",
    blockers: ["Mailcow runs on this server."],
  } as never);
  open();
  fireEvent.click(screen.getByRole("button", { name: "Plan" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Mailcow runs on this server.");
  expect(screen.queryByRole("button", { name: "Approve and install" })).not.toBeInTheDocument();
});

it("shows progress, failure, and needs the words to roll back", async () => {
  open({
    state: "failed",
    kind: "apply",
    error: "postfix check failed",
    lines: ["$ postfix check"],
  });
  expect(await screen.findByTestId("host-job")).toHaveTextContent("$ postfix check");
  expect(screen.getByText("postfix check failed")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Roll back" })).toBeDisabled();
});
