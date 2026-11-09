import * as React from "react";
import { toast } from "sonner";
import {
  Palette,
  Bell,
  Globe,
  Check,
  Moon,
  Sun,
  Laptop,
} from "lucide-react";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { useTheme } from "@/contexts/ThemeContext";
import { useLanguage } from "@/contexts/LanguageContext";

export function SettingsPage() {
  const { theme, setTheme } = useTheme();
  const { language, setLanguage } = useLanguage();

  const [notifications, setNotifications] = React.useState({
    emailAlerts: true,
    pushNotifications: true,
    weeklyDigest: false,
    securityAlerts: true,
  });

  const [preferences, setPreferences] = React.useState({
    compactMode: false,
    autoSave: true,
    soundEffects: false,
  });

  const handleToggleNotification = (key: keyof typeof notifications) => {
    setNotifications((prev) => {
      const updated = { ...prev, [key]: !prev[key] };
      toast.success("Notification preferences updated.");
      return updated;
    });
  };

  const handleTogglePreference = (key: keyof typeof preferences) => {
    setPreferences((prev) => {
      const updated = { ...prev, [key]: !prev[key] };
      toast.success("Display preferences updated.");
      return updated;
    });
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
          Application Settings
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Configure appearance, system behavior, alerts, and localized preferences.
        </p>
      </div>

      <Tabs defaultValue="appearance" className="space-y-6">
        <TabsList>
          <TabsTrigger value="appearance" className="gap-2">
            <Palette className="h-4 w-4" />
            <span>Appearance</span>
          </TabsTrigger>
          <TabsTrigger value="notifications" className="gap-2">
            <Bell className="h-4 w-4" />
            <span>Notifications</span>
          </TabsTrigger>
          <TabsTrigger value="language" className="gap-2">
            <Globe className="h-4 w-4" />
            <span>Language & Region</span>
          </TabsTrigger>
        </TabsList>

        {/* Appearance Tab */}
        <TabsContent value="appearance" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Theme Selection</CardTitle>
              <CardDescription>
                Choose how the template looks to you. Select a light or dark theme, or sync with your system.
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                {/* Light Theme Card */}
                <button
                  type="button"
                  onClick={() => {
                    setTheme("light");
                    toast.success("Theme set to Light");
                  }}
                  className={`flex flex-col items-center justify-center p-4 rounded-xl border-2 transition-all ${
                    theme === "light"
                      ? "border-primary bg-primary/5 text-primary"
                      : "border-border hover:border-border/80 text-foreground"
                  }`}
                >
                  <Sun className="h-6 w-6 mb-2" />
                  <span className="text-sm font-semibold">Light</span>
                  <span className="text-xs text-muted-foreground mt-0.5">Clean & high-contrast</span>
                  {theme === "light" && <Check className="h-4 w-4 text-primary mt-2" />}
                </button>

                {/* Dark Theme Card */}
                <button
                  type="button"
                  onClick={() => {
                    setTheme("dark");
                    toast.success("Theme set to Dark");
                  }}
                  className={`flex flex-col items-center justify-center p-4 rounded-xl border-2 transition-all ${
                    theme === "dark"
                      ? "border-primary bg-primary/5 text-primary"
                      : "border-border hover:border-border/80 text-foreground"
                  }`}
                >
                  <Moon className="h-6 w-6 mb-2" />
                  <span className="text-sm font-semibold">Dark</span>
                  <span className="text-xs text-muted-foreground mt-0.5">Easy on the eyes</span>
                  {theme === "dark" && <Check className="h-4 w-4 text-primary mt-2" />}
                </button>

                {/* System Theme Card */}
                <button
                  type="button"
                  onClick={() => {
                    setTheme("system");
                    toast.success("Theme synchronized with system preferences");
                  }}
                  className={`flex flex-col items-center justify-center p-4 rounded-xl border-2 transition-all ${
                    theme === "system"
                      ? "border-primary bg-primary/5 text-primary"
                      : "border-border hover:border-border/80 text-foreground"
                  }`}
                >
                  <Laptop className="h-6 w-6 mb-2" />
                  <span className="text-sm font-semibold">System</span>
                  <span className="text-xs text-muted-foreground mt-0.5">Sync with OS</span>
                  {theme === "system" && <Check className="h-4 w-4 text-primary mt-2" />}
                </button>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Display & Layout Preferences</CardTitle>
              <CardDescription>
                Customize UI density and operational helpers.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="compactMode" className="text-sm font-medium">
                    Compact Mode
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Reduce table padding and row heights to display more rows per screen.
                  </p>
                </div>
                <Switch
                  id="compactMode"
                  checked={preferences.compactMode}
                  onCheckedChange={() => handleTogglePreference("compactMode")}
                />
              </div>

              <div className="flex items-center justify-between border-t pt-4">
                <div className="space-y-0.5">
                  <Label htmlFor="autoSave" className="text-sm font-medium">
                    Automatic Form Drafting
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Automatically preserve active form input values in temporary state.
                  </p>
                </div>
                <Switch
                  id="autoSave"
                  checked={preferences.autoSave}
                  onCheckedChange={() => handleTogglePreference("autoSave")}
                />
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Notifications Tab */}
        <TabsContent value="notifications" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Notification Channels</CardTitle>
              <CardDescription>
                Select which channels you want to receive alerts and summaries on.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label className="text-sm font-medium">Critical Security Alerts</Label>
                  <p className="text-xs text-muted-foreground">
                    Receive urgent notifications about logins from new devices.
                  </p>
                </div>
                <Switch
                  checked={notifications.securityAlerts}
                  onCheckedChange={() => handleToggleNotification("securityAlerts")}
                />
              </div>

              <div className="flex items-center justify-between border-t pt-4">
                <div className="space-y-0.5">
                  <Label className="text-sm font-medium">Email Notifications</Label>
                  <p className="text-xs text-muted-foreground">
                    Get email alerts when records are assigned to your queue.
                  </p>
                </div>
                <Switch
                  checked={notifications.emailAlerts}
                  onCheckedChange={() => handleToggleNotification("emailAlerts")}
                />
              </div>

              <div className="flex items-center justify-between border-t pt-4">
                <div className="space-y-0.5">
                  <Label className="text-sm font-medium">Weekly Executive Digest</Label>
                  <p className="text-xs text-muted-foreground">
                    Receive a consolidated metrics summary report every Monday morning.
                  </p>
                </div>
                <Switch
                  checked={notifications.weeklyDigest}
                  onCheckedChange={() => handleToggleNotification("weeklyDigest")}
                />
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Language Tab */}
        <TabsContent value="language" className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Language & Locale</CardTitle>
              <CardDescription>
                Switch between supported languages (English and Swahili / Kiswahili).
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <button
                  type="button"
                  onClick={() => {
                    setLanguage("en");
                    toast.success("Language switched to English");
                  }}
                  className={`flex items-center justify-between p-4 rounded-xl border-2 transition-all ${
                    language === "en"
                      ? "border-primary bg-primary/5 text-primary"
                      : "border-border text-foreground hover:border-border/80"
                  }`}
                >
                  <div className="text-left">
                    <p className="font-semibold text-sm">English</p>
                    <p className="text-xs text-muted-foreground">Default system language</p>
                  </div>
                  {language === "en" && <Check className="h-5 w-5 text-primary" />}
                </button>

                <button
                  type="button"
                  onClick={() => {
                    setLanguage("sw");
                    toast.success("Lugha imebadilishwa kuwa Kiswahili");
                  }}
                  className={`flex items-center justify-between p-4 rounded-xl border-2 transition-all ${
                    language === "sw"
                      ? "border-primary bg-primary/5 text-primary"
                      : "border-border text-foreground hover:border-border/80"
                  }`}
                >
                  <div className="text-left">
                    <p className="font-semibold text-sm">Kiswahili</p>
                    <p className="text-xs text-muted-foreground">Lugha ya Kiswahili</p>
                  </div>
                  {language === "sw" && <Check className="h-5 w-5 text-primary" />}
                </button>
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}

export default SettingsPage;
