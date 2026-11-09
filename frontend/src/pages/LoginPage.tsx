import { useNavigate, useSearchParams } from "react-router-dom";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import { toast } from "sonner";
import { ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form";
import { useAuth } from "@/contexts/AuthContext";
import { useLanguage } from "@/contexts/LanguageContext";

const loginSchema = z.object({
  email: z.string().email("Enter the email on the operator account"),
  password: z.string().min(1, "Enter the password"),
});

type LoginFormValues = z.infer<typeof loginSchema>;

export function LoginPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { login, isLoading } = useAuth();
  const { t } = useLanguage();
  const redirectUrl = searchParams.get("redirect") || "/dashboard";

  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: "", password: "" },
  });

  const onSubmit = async (values: LoginFormValues) => {
    try {
      await login(values);
      toast.success("Signed in.");
      navigate(redirectUrl);
    } catch (err) {
      const status = err && typeof err === "object" && "statusCode" in err ? Number(err.statusCode) : 0;
      toast.error(status === 429 ? "Too many attempts. Wait and try again." : "That email or password was not accepted.");
    }
  };

  return (
    <div className="space-y-6">
      <div className="space-y-2 text-center">
        <h1 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">{t("login")}</h1>
        <p className="text-sm text-muted-foreground">
          Sign in with the operator account. There is no public signup, and project API keys are not used here.
        </p>
      </div>

      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
          <FormField
            control={form.control}
            name="email"
            render={({ field }) => (
              <FormItem>
                <FormLabel>Email</FormLabel>
                <FormControl>
                  <Input type="email" autoComplete="username" placeholder="you@example.com" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t("password")}</FormLabel>
                <FormControl>
                  <PasswordInput placeholder="Password" autoComplete="current-password" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button type="submit" className="w-full gap-2" disabled={isLoading}>
            {isLoading ? "Signing in..." : t("login")}
            <ArrowRight className="h-4 w-4" />
          </Button>
        </form>
      </Form>
    </div>
  );
}

export default LoginPage;
