import { useEffect, useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useAuth } from "@/features/auth/auth";
import { useI18n } from "@/features/i18n/i18n";
import { LanguageSwitcher } from "@/features/i18n/LanguageSwitcher";
import type { Theme } from "@/types";

export function SettingsView() {
  const { user } = useAuth();
  const { t } = useI18n();
  const [theme, setTheme] = useState<Theme>(
    () => (localStorage.getItem("theme") as Theme) ?? "system",
  );
  const [notifications, setNotifications] = useState(true);

  useEffect(() => {
    const update = () => setTheme((localStorage.getItem("theme") as Theme) ?? "system");
    window.addEventListener("themechange", update);
    return () => window.removeEventListener("themechange", update);
  }, []);

  const changeTheme = (value: Theme) => {
    localStorage.setItem("theme", value);
    setTheme(value);
    window.dispatchEvent(new Event("themechange"));
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t.settings.title}</h1>
        <p className="mt-2 text-muted-foreground">{t.settings.subtitle}</p>
      </div>

      <Tabs defaultValue="account" className="mt-6">
        <TabsList>
          <TabsTrigger value="account">{t.settings.account}</TabsTrigger>
          <TabsTrigger value="appearance">{t.settings.appearance}</TabsTrigger>
          <TabsTrigger value="notifications">{t.settings.notifications}</TabsTrigger>
        </TabsList>

        <TabsContent value="account" className="space-y-4 rounded-xl border bg-card p-6">
          <h2 className="font-semibold">Account Profile</h2>
          <div className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <span className="text-muted-foreground">User Name:</span>{" "}
              <strong className="text-foreground">{user?.name}</strong>
            </div>
            <div>
              <span className="text-muted-foreground">Assigned Role:</span>{" "}
              <strong className="text-foreground">{user?.role}</strong>
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            Account data is maintained locally in the demo session.
          </p>
        </TabsContent>

        <TabsContent value="appearance" className="space-y-6 rounded-xl border bg-card p-6">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div>
              <Label htmlFor="theme" className="text-base font-semibold">
                {t.settings.theme}
              </Label>
              <p className="text-sm text-muted-foreground">
                Choose between light, dark, or system mode.
              </p>
            </div>
            <select
              id="theme"
              value={theme}
              onChange={(e) => changeTheme(e.target.value as Theme)}
              className="rounded-md border bg-background px-3 py-2 text-sm"
            >
              <option value="system">System</option>
              <option value="light">Light</option>
              <option value="dark">Dark</option>
            </select>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-4 border-t pt-4">
            <div>
              <p className="text-base font-semibold">{t.settings.language}</p>
              <p className="text-sm text-muted-foreground">Select preferred interface language.</p>
            </div>
            <LanguageSwitcher />
          </div>
        </TabsContent>

        <TabsContent value="notifications" className="rounded-xl border bg-card p-6">
          <div className="flex items-center justify-between">
            <div>
              <Label htmlFor="notifications-toggle" className="text-base font-semibold">
                {t.settings.notifications}
              </Label>
              <p className="text-sm text-muted-foreground">Receive mock workspace notifications.</p>
            </div>
            <Switch
              id="notifications-toggle"
              checked={notifications}
              onCheckedChange={setNotifications}
            />
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}
