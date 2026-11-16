import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "./context";
import { Domains, DomainDetail } from "./pages/Domains";

afterEach(() => vi.restoreAllMocks());

it("never offers to copy a placeholder DNS value", async () => {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path.endsWith("/domains/d1"))
      return {
        id: "d1",
        name: "rec.example.com",
        purpose: "sending",
        provider_type: "generic",
        records: [
          {
            key: "ownership",
            type: "TXT",
            host: "rec.example.com",
            short_host: "@",
            value: "njiwa-mail-verify=abc",
            pass: true,
            found: "njiwa-mail-verify=abc",
          },
          {
            key: "spf",
            type: "TXT",
            host: "rec.example.com",
            short_host: "@",
            value: "v=spf1 <your mail server's sending IPs (ip4:/ip6:) or SPF include> -all",
            placeholder: true,
            pass: false,
            found: "(none)",
          },
        ],
        senders: [],
        mailboxes: [],
        recent_messages: [],
      };
    return {};
  });
  render(
    <MemoryRouter initialEntries={["/d1"]}>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <Routes>
            <Route path="/:id" element={<DomainDetail />} />
          </Routes>
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  const tpl = (await screen.findByText(/Template, not a record to copy/)).closest(
    "dd",
  ) as HTMLElement;
  expect(tpl).toHaveAttribute("data-placeholder", "true");
  expect(within(tpl).queryByRole("button", { name: /Copy/ })).not.toBeInTheDocument();
  // A real record value can still be copied.
  expect(screen.getAllByRole("button", { name: "Copy value" })).toHaveLength(1);
});

function renderDomain(extra: Record<string, unknown>) {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path.endsWith("/domains/d1"))
      return {
        id: "d1",
        name: "notify.example.com",
        purpose: "sending",
        provider_type: "generic",
        records: [],
        senders: [],
        mailboxes: [],
        recent_messages: [],
        ...extra,
      };
    return {};
  });
  render(
    <MemoryRouter initialEntries={["/d1"]}>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <Routes>
            <Route path="/:id" element={<DomainDetail />} />
          </Routes>
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("does not call a published key proof of signing, and checks a pasted message", async () => {
  renderDomain({ dkim_verified_at: null });
  expect(await screen.findByTestId("dkim-proof")).toHaveTextContent("Not proven yet");
  const send = vi.spyOn(admin, "send").mockResolvedValue({
    from_domain: "notify.example.com",
    aligned: true,
    recorded: true,
    message:
      "The DKIM signature is valid and aligned with notify.example.com (d=notify.example.com, s=dkim).",
    signatures: [{ d: "notify.example.com", s: "dkim", result: "pass" }],
  } as never);
  fireEvent.change(screen.getByLabelText("Received message, headers included"), {
    target: { value: "DKIM-Signature: v=1\nFrom: a@notify.example.com\n\nbody" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Check signature" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/domains/d1/verify-message", "POST", {
      raw: "DKIM-Signature: v=1\nFrom: a@notify.example.com\n\nbody",
    }),
  );
  expect(await screen.findByText(/signature is valid and aligned/)).toBeInTheDocument();
});

it("shows recorded signed-mail evidence", async () => {
  renderDomain({ dkim_verified_at: "2026-11-12T08:00:00Z", dkim_verified_selector: "dkim" });
  expect(await screen.findByTestId("dkim-proof")).toHaveTextContent("Signed mail verified");
});

it("saves the existing server's DKIM key for the domain", async () => {
  renderDomain({ dkim_verified_at: null, sending_profile_id: "p1", dkim_selector: "s1" });
  const send = vi.spyOn(admin, "send").mockResolvedValue({} as never);
  fireEvent.change(await screen.findByLabelText("DKIM public key"), {
    target: { value: " v=DKIM1; k=rsa; p=ABC " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save key" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/installation/transport-profiles/p1/sending-facts", "PUT", {
      dkim_keys: { "notify.example.com": "v=DKIM1; k=rsa; p=ABC" },
    }),
  );
});

it("says when delivery settings changed since the last check", async () => {
  renderDomain({ requirements_changed: true });
  const note = await screen.findByText(/Delivery settings changed since the last check/);
  expect(note).toHaveAttribute("role", "status");
});

it("shows no change notice for a current check", async () => {
  renderDomain({ requirements_changed: false });
  await screen.findByTestId("dkim-proof");
  expect(screen.queryByText(/Delivery settings changed/)).not.toBeInTheDocument();
});

function renderDomainList() {
  const get = vi.spyOn(admin, "get").mockResolvedValue({
    items: [{ id: "d1", name: "notify.example.com", purpose: "sending", bulk: false }],
    has_more: false,
    limit: 50,
    offset: 0,
  } as never);
  render(
    <MemoryRouter>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <Domains />
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  return get;
}

it("edits domain settings from the list and refreshes after saving", async () => {
  const get = renderDomainList();
  const send = vi.spyOn(admin, "send").mockResolvedValue({ id: "d1" } as never);
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const dialog = screen.getByRole("dialog");
  expect(within(dialog).getByLabelText("Domain")).toHaveAttribute("readonly");
  fireEvent.change(within(dialog).getByLabelText("Purpose"), { target: { value: "both" } });
  fireEvent.click(within(dialog).getByRole("checkbox"));
  fireEvent.click(within(dialog).getByRole("button", { name: "Save domain" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/domains/d1", "PATCH", {
      purpose: "both",
      bulk: true,
    }),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(get.mock.calls.length).toBeGreaterThan(1);
});

it("requires confirmation before deleting a domain from the list", async () => {
  renderDomainList();
  const send = vi.spyOn(admin, "send").mockResolvedValue(undefined);
  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
  expect(send).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(send).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  fireEvent.click(screen.getByRole("button", { name: "Delete domain" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/domains/d1", "DELETE", undefined),
  );
});

it("keeps an unsuccessful edit open and shows the error", async () => {
  renderDomainList();
  vi.spyOn(admin, "send").mockRejectedValue(new Error("Domain is in use"));
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  fireEvent.click(screen.getByRole("button", { name: "Save domain" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Domain is in use");
  expect(screen.getByRole("dialog")).toBeInTheDocument();
});
