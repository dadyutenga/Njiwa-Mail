import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { WorkspaceProvider } from "./context";
import { DnsSetup } from "./pages/DnsSetup";

afterEach(() => vi.restoreAllMocks());

const conn = {
  id: "c1",
  auth_method: "token",
  label: "main",
  status: "active",
  status_detail: "",
  zone_count: 1,
};

function plan(over: Record<string, unknown> = {}) {
  return {
    items: [
      {
        key: "ownership",
        action: "create",
        type: "TXT",
        name: "notify.example.com",
        content: "njiwa-mail-verify=t",
        risk: "low",
        reason: "Adds a TXT record that proves the domain is yours.",
      },
      {
        key: "spf",
        action: "modify",
        type: "TXT",
        name: "notify.example.com",
        risk: "high",
        ack: "modify_spf",
        content: "v=spf1 include:_spf.google.com mx ip4:93.184.216.34 ~all",
        existing: { id: "r1", content: "v=spf1 include:_spf.google.com ~all", ttl: 300 },
        reason: "A domain may have only one SPF record.",
      },
      {
        key: "mx",
        action: "conflict",
        type: "MX",
        name: "notify.example.com",
        content: "mail.example.com",
        risk: "high",
        others: [{ content: "aspmx.l.google.com" }],
        reason: "already receives mail through aspmx.l.google.com",
      },
      {
        key: "dkim",
        action: "manual",
        type: "TXT",
        name: "dkim._domainkey.notify.example.com",
        content: "v=DKIM1; k=rsa; p=<key>",
        placeholder: true,
        risk: "low",
        reason: "does not know this value",
      },
    ],
    warnings: [
      {
        code: "existing_mail",
        severity: "warning",
        message: "notify.example.com already has email set up (Google Workspace).",
      },
    ],
    existing_mail: ["Google Workspace"],
    blocked: false,
    required_acks: ["modify_spf"],
    rollback: "Rolling back removes only records Njiwa-Mail created.",
    ...over,
  };
}

function setupRow(status: string, over: Record<string, unknown> = {}) {
  return {
    id: "s1",
    status,
    zone_name: "example.com",
    connection_id: "c1",
    connection_status: "active",
    plan: plan(),
    digest: "dg1",
    changes: [],
    approved_by: "admin@x.test",
    ...over,
  };
}

function renderWizard(state: {
  conns?: unknown[];
  setup?: unknown;
  caps?: unknown;
  verify?: unknown;
}) {
  vi.spyOn(admin, "get").mockImplementation(async (path: string) => {
    if (path.endsWith("/dns/capabilities"))
      return state.caps ?? { cloudflare: { token: true, oauth: false, domain_connect: false } };
    if (path.endsWith("/dns/connections")) return { items: state.conns ?? [] };
    if (path.endsWith("/dns-setup")) return { setup: state.setup ?? null };
    if (path.includes("/zones"))
      return {
        items: [
          { id: "z1", name: "example.com", status: "active" },
          { id: "z2", name: "other.org", status: "active" },
        ],
      };
    if (path.endsWith("/dns-manual"))
      return {
        note: "",
        records: [
          {
            key: "spf",
            type: "TXT",
            host: "notify.example.com",
            short_host: "notify",
            value: "v=spf1 mx -all",
          },
          {
            key: "dkim",
            type: "TXT",
            host: "dkim._domainkey.notify.example.com",
            short_host: "dkim._domainkey.notify",
            value: "v=DKIM1; p=<key>",
            placeholder: true,
          },
        ],
      };
    return {};
  });
  const send = vi.spyOn(admin, "send").mockImplementation(async (path: string) => {
    if (path.endsWith("/verify"))
      return (state.verify ?? {
        state: "pending",
        ready: false,
        message: "Waiting for DNS to propagate.",
        signing_proven: false,
        items: [],
      }) as never;
    return {} as never;
  });
  render(
    <MemoryRouter initialEntries={["/"]}>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <WorkspaceProvider project="demo">
          <Routes>
            <Route
              path="/"
              element={<DnsSetup domainId="d1" domain="notify.example.com" onChanged={() => {}} />}
            />
          </Routes>
        </WorkspaceProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
  return send;
}

const current = () =>
  screen.getByRole("list", { name: "DNS setup progress" }).querySelector('[aria-current="step"]')
    ?.textContent ?? "";

it("starts at Connect Cloudflare, never asks for a password, and offers a manual fallback", async () => {
  renderWizard({});
  expect(
    await screen.findByText(/Create a token in Cloudflare with Zone → DNS → Edit/),
  ).toBeInTheDocument();
  expect(current()).toContain("Connect Cloudflare");
  expect(screen.getByLabelText("API token")).toHaveAttribute("type", "password");
  expect(screen.queryByText(/password:/i)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Sign in with Cloudflare" })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "My DNS is somewhere else" }));
  expect(await screen.findByText(/Publish these records at your DNS provider/)).toBeInTheDocument();
  // A real value can be copied; a template cannot.
  expect(await screen.findAllByRole("button", { name: "Copy value" })).toHaveLength(1);
  expect(screen.getByText(/Template, not a record to copy/)).toBeInTheDocument();
});

it("connects with a token and clears it from the form", async () => {
  const send = renderWizard({});
  const input = await screen.findByLabelText("API token");
  fireEvent.change(input, { target: { value: "tok_secret_value_0123456789" } });
  fireEvent.click(screen.getByRole("button", { name: "Connect with token" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/dns/connections", "POST", {
      provider: "cloudflare",
      token: "tok_secret_value_0123456789",
      label: "",
    }),
  );
  await waitFor(() => expect(screen.getByLabelText("API token")).toHaveValue(""));
});

it("offers OAuth only when the installation has it, and follows only an https URL", async () => {
  const assign = vi.fn();
  vi.stubGlobal("location", { ...window.location, assign });
  const send = renderWizard({
    caps: { cloudflare: { token: true, oauth: true, domain_connect: false } },
  });
  send.mockResolvedValue({ url: "javascript:alert(1)" } as never);
  fireEvent.click(await screen.findByRole("button", { name: "Sign in with Cloudflare" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/dns/oauth/start", "POST", { domain_id: "d1" }),
  );
  expect(assign).not.toHaveBeenCalled();
  send.mockResolvedValue({ url: "https://dash.cloudflare.example/authorize?x=1" } as never);
  fireEvent.click(screen.getByRole("button", { name: "Sign in with Cloudflare" }));
  await waitFor(() =>
    expect(assign).toHaveBeenCalledWith("https://dash.cloudflare.example/authorize?x=1"),
  );
  vi.unstubAllGlobals();
});

it("preselects the zone that contains the domain and analyzes it", async () => {
  const send = renderWizard({ conns: [conn] });
  const select = (await screen.findByLabelText("DNS zone")) as HTMLSelectElement;
  await waitFor(() => expect(select.value).toBe("z1"));
  expect(current()).toContain("Select zone");
  fireEvent.click(screen.getByRole("button", { name: "Analyze this zone" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/domains/d1/dns-setup", "POST", {
      connection_id: "c1",
      zone_id: "z1",
    }),
  );
});

it("shows exactly what changes, and approval needs the confirmation and sends the reviewed digest", async () => {
  const send = renderWizard({ conns: [conn], setup: setupRow("analyzed") });
  expect(await screen.findByText(/Will be created \(1\)/)).toBeInTheDocument();
  expect(screen.getByText(/Will be changed \(1\)/)).toBeInTheDocument();
  expect(screen.getByText(/Needs you: conflicts \(1\)/)).toBeInTheDocument();
  expect(screen.getByText(/Cannot be automated \(1\)/)).toBeInTheDocument();
  expect(screen.getByText("v=spf1 include:_spf.google.com ~all")).toBeInTheDocument(); // before
  expect(
    screen.getByText("v=spf1 include:_spf.google.com mx ip4:93.184.216.34 ~all"),
  ).toBeInTheDocument(); // after
  expect(screen.getByText(/Google Workspace/, { selector: "strong" })).toBeInTheDocument();
  expect(
    screen.getByText(/Rolling back removes only records Njiwa-Mail created/),
  ).toBeInTheDocument();
  expect(current()).toContain("Approve");
  const approve = screen.getByRole("button", { name: "Approve these exact changes" });
  expect(approve).toBeDisabled();
  fireEvent.click(
    screen.getByRole("checkbox", { name: /existing SPF record will be edited in place/ }),
  );
  expect(approve).toBeEnabled();
  fireEvent.click(approve);
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/dns-setups/s1/approve", "POST", {
      digest: "dg1",
      acknowledge: ["modify_spf"],
    }),
  );
});

it("cannot approve a blocked plan or one with nothing to write", async () => {
  renderWizard({
    conns: [conn],
    setup: setupRow("blocked", {
      plan: plan({
        blocked: true,
        warnings: [
          {
            code: "not_delegated",
            severity: "blocker",
            message:
              "example.com exists in Cloudflare, but the internet does not use Cloudflare for it.",
          },
        ],
      }),
    }),
  });
  expect(await screen.findByRole("alert")).toHaveTextContent(/does not use Cloudflare/);
  expect(screen.getByRole("button", { name: "Approve these exact changes" })).toBeDisabled();
  expect(screen.getByText(/Fix the blocking problem above/)).toBeInTheDocument();
});

it("an approved plan is applied only after a confirmation that says what is written", async () => {
  const send = renderWizard({ conns: [conn], setup: setupRow("approved") });
  fireEvent.click(await screen.findByRole("button", { name: "Apply to Cloudflare" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(
    within(dialog).getByText(/If DNS changed since you reviewed it, nothing is written/),
  ).toBeInTheDocument();
  expect(send).not.toHaveBeenCalledWith(expect.stringContaining("/apply"), "POST", undefined);
  fireEvent.click(within(dialog).getByRole("button", { name: "Apply changes" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/dns-setups/s1/apply", "POST", undefined),
  );
});

it("a partly applied setup is never shown as done", async () => {
  renderWizard({
    conns: [conn],
    setup: setupRow("partial", {
      changes: [
        {
          id: "a",
          key: "ownership",
          action: "create",
          type: "TXT",
          name: "n.example.com",
          status: "applied",
          note: "",
          adopted: false,
        },
        {
          id: "b",
          key: "mx",
          action: "create",
          type: "MX",
          name: "n.example.com",
          status: "failed",
          note: "",
          adopted: false,
        },
      ],
    }),
  });
  expect(await screen.findByText(/Only some of the records were written/)).toBeInTheDocument();
  expect(current()).toContain("Apply");
  expect(screen.queryByText("Ready", { selector: "[aria-current] *" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Roll back" })).toBeInTheDocument();
});

it("after apply it reports Pending, not Ready, until DNS answers", async () => {
  renderWizard({
    conns: [conn],
    setup: setupRow("applied"),
    verify: {
      state: "pending",
      ready: false,
      message: "Waiting for DNS to propagate. Njiwa-Mail keeps checking.",
      signing_proven: false,
      items: [
        {
          key: "spf",
          type: "TXT",
          name: "notify.example.com",
          state: "pending",
          detail: "Published, but not visible in public DNS yet.",
        },
      ],
    },
  });
  expect(await screen.findByText(/Waiting for DNS to propagate/)).toBeInTheDocument();
  expect(screen.getAllByText("Pending").length).toBeGreaterThan(0);
  expect(current()).toContain("Verify");
});

it("is Ready only when verification says so, and still does not promise delivery", async () => {
  renderWizard({
    conns: [conn],
    setup: setupRow("applied"),
    verify: {
      state: "verified",
      ready: true,
      signing_proven: false,
      message:
        "DNS checks pass for this domain. That does not guarantee inbox delivery: send a test message first.",
      items: [
        {
          key: "spf",
          type: "TXT",
          name: "notify.example.com",
          state: "verified",
          detail: "Seen in public DNS and valid.",
        },
      ],
    },
  });
  expect(await screen.findByText(/does not guarantee inbox delivery/)).toBeInTheDocument();
  await waitFor(() => expect(current()).toContain("Ready"));
  expect(screen.getByText(/does not prove your mail is signed/)).toBeInTheDocument();
});

it("disconnecting says the DNS records stay", async () => {
  const send = renderWizard({ conns: [conn] });
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(
    within(dialog).getByText(/stay exactly as they are; they are not removed/),
  ).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }));
  await waitFor(() =>
    expect(send).toHaveBeenCalledWith("/w/demo/live/dns/connections/c1", "DELETE", undefined),
  );
});

it("a rejected connection asks to connect again", async () => {
  renderWizard({ conns: [{ ...conn, status: "error" }] });
  expect(await screen.findByLabelText("API token")).toBeInTheDocument();
  expect(current()).toContain("Connect Cloudflare");
});
