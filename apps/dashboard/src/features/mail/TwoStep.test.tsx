import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { admin } from "@/lib/admin-api";
import { TwoStep } from "./TwoStep";

afterEach(() => vi.restoreAllMocks());

describe("TwoStep", () => {
  it("enrols, confirms with a code and shows recovery codes once", async () => {
    const send = vi
      .spyOn(admin, "send")
      .mockResolvedValueOnce({ secret: "JBSWY3DPEHPK3PXP", otpauth_uri: "otpauth://totp/x" })
      .mockResolvedValueOnce({ recovery_codes: ["AAAAA-BBBBB", "CCCCC-DDDDD"] });
    const changed = vi.fn();
    render(<TwoStep enabled={false} onChanged={changed} />);
    fireEvent.click(screen.getByRole("button", { name: "Set up two-step sign-in" }));
    expect(await screen.findByTestId("mfa-secret")).toHaveTextContent("JBSWY3DPEHPK3PXP");
    fireEvent.change(screen.getByLabelText("Code from the app"), { target: { value: "123456" } });
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    expect(await screen.findByText("AAAAA-BBBBB")).toBeInTheDocument();
    expect(send).toHaveBeenLastCalledWith("/auth/mfa/confirm", "POST", { code: "123456" });
    expect(changed).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "I saved them" }));
    expect(screen.queryByText("AAAAA-BBBBB")).not.toBeInTheDocument();
  });

  it("shows the server's error and does not enable on a wrong code", async () => {
    vi.spyOn(admin, "send")
      .mockResolvedValueOnce({ secret: "S", otpauth_uri: "otpauth://totp/x" })
      .mockRejectedValueOnce(new Error("invalid_mfa_code"));
    render(<TwoStep enabled={false} onChanged={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Set up two-step sign-in" }));
    fireEvent.change(await screen.findByLabelText("Code from the app"), {
      target: { value: "000000" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Turn on" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("invalid_mfa_code"));
  });

  it("turns off with password and code when enabled", async () => {
    const send = vi.spyOn(admin, "send").mockResolvedValue(undefined);
    render(<TwoStep enabled onChanged={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "correct-horse-battery" },
    });
    fireEvent.change(screen.getByLabelText("Authentication code"), { target: { value: "654321" } });
    fireEvent.click(screen.getByRole("button", { name: "Turn off" }));
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("/auth/mfa/disable", "POST", {
        password: "correct-horse-battery",
        code: "654321",
      }),
    );
  });
});
