import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/extra-inputs";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { useAuth } from "./auth";
import { useI18n } from "@/features/i18n/i18n";

const authSchema = z.object({
  email: z.string().trim().email("Please enter a valid email address"),
  password: z
    .string()
    .min(8, "Password must be at least 8 characters")
    .optional()
    .or(z.literal("")),
  confirmPassword: z.string().optional().or(z.literal("")),
});

type AuthFormValues = z.infer<typeof authSchema>;

export function AuthForm({ mode }: { mode: string }) {
  const [feedback, setFeedback] = useState<string | null>(null);
  const { login } = useAuth();
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();

  const form = useForm<AuthFormValues>({
    resolver: zodResolver(authSchema),
    defaultValues: {
      email: "",
      password: "",
      confirmPassword: "",
    },
  });

  const title =
    mode === "login"
      ? t.auth.welcomeBack
      : mode === "register"
        ? t.auth.createAccount
        : mode === "reset-password"
          ? t.auth.resetPassword
          : t.auth.forgotPassword;

  const submitText =
    mode === "login"
      ? t.actions.signIn
      : mode === "register"
        ? t.actions.register
        : "Send Instructions";

  const onSubmit = (values: AuthFormValues) => {
    if (mode === "login" || mode === "register") {
      const name = values.email.split("@")[0] || "Demo User";
      login(name);
      const state = location.state as { from?: string } | null;
      navigate(state?.from || "/", { replace: true });
    } else {
      setFeedback("Instructions sent (mock demo: no email was dispatched).");
    }
  };

  return (
    <div className="w-full">
      <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
      <p className="mt-2 text-sm text-muted-foreground">{t.auth.demoNotice}</p>

      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className="mt-8 space-y-4">
          <FormField
            control={form.control}
            name="email"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t.auth.emailLabel}</FormLabel>
                <FormControl>
                  <Input placeholder="user@example.com" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          {mode !== "forgot-password" && (
            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t.auth.passwordLabel}</FormLabel>
                  <FormControl>
                    <PasswordInput placeholder="••••••••" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}

          {feedback && (
            <p role="status" className="rounded-lg bg-primary/10 p-3 text-sm text-primary">
              {feedback}
            </p>
          )}

          <Button type="submit" className="w-full">
            {submitText}
          </Button>
        </form>
      </Form>

      <div className="mt-6 flex flex-wrap gap-4 text-sm text-muted-foreground">
        {mode !== "login" && (
          <Link to="/login" className="hover:text-primary">
            {t.actions.signIn}
          </Link>
        )}
        {mode !== "register" && (
          <Link to="/register" className="hover:text-primary">
            {t.actions.register}
          </Link>
        )}
        {mode !== "forgot-password" && (
          <Link to="/forgot-password" className="hover:text-primary">
            {t.auth.forgotPassword}?
          </Link>
        )}
      </div>
    </div>
  );
}
