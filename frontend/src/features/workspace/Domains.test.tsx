import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "./context";
import { DomainDetail } from "./pages/Domains";

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
