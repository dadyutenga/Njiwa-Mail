import { useAuth } from "@/features/auth/auth";
import { usePhrase } from "@/features/i18n/i18n";
import { TwoStep } from "./TwoStep";
import { AccountSecurity } from "./AccountSecurity";

export default function Profile() {
  const { user, refresh } = useAuth();
  const phrase = usePhrase();
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">{phrase("Profile")}</h1>
      <section className="rounded-xl border bg-card p-5">
        <p>
          <strong>{phrase("Email:")}</strong> {user?.email}
        </p>
        <p>
          <strong>{phrase("Role:")}</strong> {user?.role}
        </p>
      </section>
      <TwoStep enabled={!!user?.mfa_enabled} onChanged={refresh} />
      <AccountSecurity
        mfa={!!user?.mfa_enabled}
        deletionDueAt={user?.deletion_due_at}
        onChanged={refresh}
      />
    </div>
  );
}
