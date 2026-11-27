import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "@/features/auth/auth";
import { admin, type AdminUser } from "@/lib/admin-api";
import { passwordStrength, Setup, SetupGate } from "./Setup";

afterEach(() => {
  vi.restoreAllMocks();
});

const user = { csrf_token: "c", email: "asha@acme.tz", mfa_enabled: false } as AdminUser;

function open(path: string, needs: boolean, token = false) {
  vi.spyOn(admin, "setupStatus").mockResolvedValue({ needs_setup: needs, token_required: token });
  vi.spyOn(admin, "me").mockRejectedValue(new Error("no session"));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter initialEntries={[path]}>
      <QueryClientProvider client={client}>
        <AuthProvider>
          <Routes>
            <Route element={<SetupGate />}>
              <Route path="/setup" element={<Setup />} />
              <Route path="/login" element={<p>sign-in page</p>} />
              <Route path="*" element={<p>some page</p>} />
            </Route>
          </Routes>
        </AuthProvider>
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

it("sends every visit to the setup while there is no user", async () => {
  open("/p/demo/dashboard", true);
  expect(await screen.findByText("Welcome to Njiwa-Mail")).toBeInTheDocument();
});

it("sends /setup to sign-in once the installation is set up", async () => {
  open("/setup", false);
  expect(await screen.findByText("sign-in page")).toBeInTheDocument();
});

it("walks the steps, goes back, creates the account and offers the first project", async () => {
  const setup = vi.spyOn(admin, "setup").mockResolvedValue(user);
  const send = vi.spyOn(admin, "send").mockResolvedValue(undefined as never);
  open("/setup", true, true);
  fireEvent.click(await screen.findByRole("button", { name: "Start" }));
  const next = () => fireEvent.click(screen.getByRole("button", { name: "Next" }));
  next();
  expect(screen.getByRole("alert")).toHaveTextContent("at least 2 characters");
  fireEvent.change(screen.getByLabelText("Organization name"), { target: { value: "Acme" } });
  next();
  fireEvent.click(screen.getByRole("button", { name: "Back" }));
  expect(screen.getByLabelText("Organization name")).toHaveValue("Acme");
  next();
  fireEvent.change(screen.getByLabelText("Your name"), { target: { value: "Asha Juma" } });
  fireEvent.change(screen.getByLabelText("Email"), { target: { value: "asha@acme.tz" } });
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "short" } });
  next();
  expect(screen.getByRole("alert")).toHaveTextContent("at least 12 characters");
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "a-long-password-42" },
  });
  next();
  expect(screen.getByRole("alert")).toHaveTextContent("setup token");
  fireEvent.change(screen.getByLabelText(/Setup token/), { target: { value: "tok-1234567890" } });
  next();
  expect(screen.getByText("asha@acme.tz")).toBeInTheDocument();
  expect(screen.queryByText("a-long-password-42")).not.toBeInTheDocument();
  vi.spyOn(admin, "me").mockResolvedValue(user);
  fireEvent.click(screen.getByRole("button", { name: "Create account" }));
  await waitFor(() =>
    expect(setup).toHaveBeenCalledWith({
      organization: "Acme",
      name: "Asha Juma",
      email: "asha@acme.tz",
      password: "a-long-password-42",
      token: "tok-1234567890",
    }),
  );
  // Still on the setup although the installation is now set up.
  expect(await screen.findByText("Your account is ready")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText(/Create your first project/), {
    target: { value: "shop" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create project" }));
  await waitFor(() => expect(send).toHaveBeenCalledWith("/projects", "POST", { name: "shop" }));
  expect(await screen.findByRole("link", { name: "Add your team" })).toHaveAttribute(
    "href",
    "/p/shop/users",
  );
});

it("rates passwords", () => {
  expect(passwordStrength("elevenchars")).toBe("short");
  expect(passwordStrength("twelve chars")).toBe("fair");
  expect(passwordStrength("a-very-long-passphrase-indeed")).toBe("strong");
});
