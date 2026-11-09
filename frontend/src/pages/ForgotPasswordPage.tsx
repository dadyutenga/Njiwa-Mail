import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api";

export function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [pending, setPending] = useState(false);
  const [sent, setSent] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    try {
      await apiClient.post("/auth/forgot", { email });
      setSent(true);
    } catch {
      toast.error("The request was not accepted.");
    } finally {
      setPending(false);
    }
  };

  if (sent) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold tracking-tight">Check the inbox</h1>
        <p className="text-sm text-muted-foreground">If that account can receive mail, the reset link is on its way.</p>
        <Button variant="outline" asChild className="w-full">
          <Link to="/login">Sign in</Link>
        </Button>
      </div>
    );
  }

  return (
    <form className="space-y-4" onSubmit={submit}>
      <h1 className="text-2xl font-semibold tracking-tight">Reset password</h1>
      <Input type="email" autoComplete="username" required value={email} onChange={(event) => setEmail(event.target.value)} placeholder="Email" />
      <Button type="submit" className="w-full" disabled={pending}>Send link</Button>
      <Button variant="ghost" asChild className="w-full">
        <Link to="/login">Sign in</Link>
      </Button>
    </form>
  );
}

export default ForgotPasswordPage;
