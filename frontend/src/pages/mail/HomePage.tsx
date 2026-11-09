import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";

export function HomePage() {
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <div className="max-w-lg space-y-4 text-center">
        <h1 className="text-3xl font-bold tracking-tight">Ditronics Mail</h1>
        <p className="text-sm text-muted-foreground">
          Operator console for outbound mail, inbound mail, bounces and webhooks. The strongest positive
          state is submitted: the SMTP server accepted the message. There is no delivered status.
        </p>
        <Button asChild>
          <Link to="/login">Sign in</Link>
        </Button>
      </div>
    </div>
  );
}

export function NoSignupPage() {
  return (
    <div className="space-y-4 text-center">
      <h1 className="text-2xl font-bold">Accounts are created by an owner</h1>
      <p className="text-sm text-muted-foreground">
        There is no public signup. An owner creates accounts. Password reset is available from sign in.
      </p>
      <Button asChild variant="outline">
        <Link to="/login">Back to sign in</Link>
      </Button>
    </div>
  );
}

export default HomePage;
