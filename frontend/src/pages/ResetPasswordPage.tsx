import { useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { PasswordInput } from "@/components/ui/password-input";
import { apiClient } from "@/lib/api";

export function ResetPasswordPage() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const token = params.get("token") ?? "";
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    try {
      await apiClient.post("/auth/reset", { token, password });
      toast.success("Password updated.");
      navigate("/login");
    } catch (error) {
      const message = error && typeof error === "object" && "message" in error ? String(error.message) : "That reset link was not accepted.";
      toast.error(message);
    } finally {
      setPending(false);
    }
  };

  if (!token) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold tracking-tight">Reset password</h1>
        <Button variant="outline" asChild className="w-full">
          <Link to="/forgot-password">Request a link</Link>
        </Button>
      </div>
    );
  }

  return (
    <form className="space-y-4" onSubmit={submit}>
      <h1 className="text-2xl font-semibold tracking-tight">New password</h1>
      <PasswordInput autoComplete="new-password" required minLength={12} value={password} onChange={(event) => setPassword(event.target.value)} placeholder="At least 12 characters" />
      <Button type="submit" className="w-full" disabled={pending || password.length < 12}>Save</Button>
    </form>
  );
}

export default ResetPasswordPage;
