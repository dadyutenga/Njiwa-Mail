import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "@/features/workspace/context";
import { Senders } from "./Senders";

afterEach(() => {
  vi.restoreAllMocks();
});

const rows = [
  {
    id: "a",
    email: "hello@example.org",
    purpose: "transactional",
    status: "claimed_elsewhere",
    domain: "example.org",
    is_default: true,
    messages: 12,
    reply_to: "help@example.org",
  },
  {
    id: "b",
    email: "news@example.org",
    purpose: "marketing",
    status: "verified",
    domain: "example.org",
    messages: 0,
  },
];

function open() {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path.includes("/usage"))
      return { api_keys: [{ id: "abc", name: "billing" }], templates: [] } as never;
    if (path.includes("/senders")) return { items: rows } as never;
    return { items: [] } as never;
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={client}>
        <WorkspaceProvider project="demo">
          <Senders />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("shows purpose, reply-to, counts and why a sender is blocked", async () => {
  open();
  expect(await screen.findByText("hello@example.org")).toBeInTheDocument();
  expect(screen.getByText(/another project holds this domain/)).toBeInTheDocument();
  expect(screen.getByText(/help@example\.org/)).toBeInTheDocument();
  expect(screen.getByText("Marketing and newsletters")).toBeInTheDocument();
  expect(screen.getByText("12")).toBeInTheDocument();
});

it("asks for a replacement before deleting a sender that is in use", async () => {
  const send = vi.spyOn(admin, "send").mockResolvedValue(undefined as never);
  open();
  await screen.findByText("hello@example.org");
  fireEvent.click(screen.getAllByRole("button", { name: "Delete" })[0]);
  expect(await screen.findByTestId("sender-usage")).toHaveTextContent("billing");
  const confirm = screen.getByRole("button", { name: "Delete sender" });
  expect(confirm).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Replace with"), { target: { value: "b" } });
  fireEvent.click(confirm);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/senders/a", "DELETE", { replace_with: "b" }),
  );
});
