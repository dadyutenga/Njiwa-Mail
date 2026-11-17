import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { admin } from "@/lib/admin-api";
import { ExistingMailServerSetup } from "./ExistingMailServerSetup";

afterEach(() => vi.restoreAllMocks());
it("connects using a mailbox and sends a test without inbound configuration", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({ smtp_port: 587, smtp_tls_mode: "starttls" });
  const send = vi
    .spyOn(admin, "send")
    .mockResolvedValueOnce({
      ok: true,
      saved: true,
      steps: [{ key: "sender", title: "Sender allowed", ok: true, detail: "accepted" }],
    })
    .mockResolvedValueOnce({ state: "submitted" });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ExistingMailServerSetup />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Connect and test" })).toBeEnabled(),
  );
  fireEvent.change(screen.getByLabelText("SMTP hostname"), {
    target: { value: "mail.example.com" },
  });
  fireEvent.change(screen.getByLabelText("Mailbox username"), {
    target: { value: "box@example.com" },
  });
  fireEvent.change(screen.getByLabelText("Mailbox password"), { target: { value: "password" } });
  fireEvent.click(screen.getByRole("button", { name: "Connect and test" }));
  await screen.findByText("Connected. Send a test email below.");
  expect(send).toHaveBeenCalledWith(
    "/mail-server/connect",
    "POST",
    expect.objectContaining({
      smtp_host: "mail.example.com",
      smtp_username: "box@example.com",
      smtp_port: 587,
      smtp_tls_mode: "starttls",
    }),
  );
  fireEvent.change(screen.getByLabelText("Test recipient"), {
    target: { value: "to@example.com" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send test email" }));
  await screen.findByText(/Your mail server accepted the test email/);
});

it("shows the failed certificate step and keeps test sending unavailable", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({
    smtp_host: "mail.example.com",
    smtp_username: "box@example.com",
    smtp_password_set: true,
  });
  vi.spyOn(admin, "send").mockResolvedValue({
    ok: false,
    saved: false,
    steps: [
      {
        key: "tls",
        title: "TLS certificate valid",
        ok: false,
        code: "SMTP_TLS_CERT_INVALID",
        detail: "Install a trusted certificate with a SAN for this hostname.",
      },
    ],
  });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ExistingMailServerSetup />
    </QueryClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Connect and test" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Connect and test" }));
  await screen.findByText("Connection failed. Your saved settings have not changed.");
  expect(screen.getByText(/Install a trusted certificate/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Send test email" })).not.toBeInTheDocument();
});

it("shows a connected server instead of asking for its details again", async () => {
  vi.spyOn(admin, "get").mockResolvedValue({
    smtp_host: "mail.example.com",
    smtp_port: 587,
    smtp_tls_mode: "starttls",
    smtp_username: "box@example.com",
    smtp_password_set: true,
    provider_type: "generic",
    last_test: {
      ok: true,
      kind: "connection",
      detail: [{ key: "login", title: "Login accepted", ok: true, detail: "ok" }],
    },
    last_test_at: "2026-11-17T12:48:22Z",
  });
  const send = vi.spyOn(admin, "send").mockResolvedValue({ ok: true, steps: [] });
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ExistingMailServerSetup />
    </QueryClientProvider>,
  );
  await screen.findByText("Mail server connected");
  expect(screen.getByText("mail.example.com:587 · STARTTLS")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Connect and test" })).toBeNull();
  // Sending a test is available straight away.
  expect(screen.getByLabelText("Test recipient")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Check again" }));
  await waitFor(() => expect(send).toHaveBeenCalledWith("/mail-server/test", "POST"));

  fireEvent.click(screen.getByRole("button", { name: "Change settings" }));
  expect(await screen.findByRole("button", { name: "Connect and test" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("button", { name: "Connect and test" })).toBeNull();
});
