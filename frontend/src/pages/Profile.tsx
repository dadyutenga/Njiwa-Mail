import { useAuth } from "@/features/auth/auth";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/features/i18n/i18n";

export default function Profile() {
  const { user } = useAuth();
  const { t } = useI18n();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t.nav.profile}</h1>
        <p className="mt-2 text-muted-foreground">Manage your personal information and session.</p>
      </div>

      <div className="rounded-2xl border bg-card p-6">
        <div className="flex items-center gap-4">
          <Avatar className="h-16 w-16 text-2xl font-bold">
            <AvatarFallback>{user?.name?.[0]?.toUpperCase() ?? "U"}</AvatarFallback>
          </Avatar>
          <div>
            <h2 className="text-xl font-semibold">{user?.name}</h2>
            <div className="mt-1 flex items-center gap-2">
              <Badge variant="secondary">{user?.role ?? "member"}</Badge>
              <span className="text-xs text-muted-foreground">Local Session</span>
            </div>
          </div>
        </div>

        <div className="mt-8 grid gap-4 border-t pt-6 text-sm sm:grid-cols-2">
          <div>
            <p className="font-medium text-muted-foreground">Full Name</p>
            <p className="mt-1 font-semibold">{user?.name}</p>
          </div>
          <div>
            <p className="font-medium text-muted-foreground">Session Status</p>
            <p className="mt-1 font-semibold text-primary">Authenticated</p>
          </div>
        </div>
      </div>
    </div>
  );
}
