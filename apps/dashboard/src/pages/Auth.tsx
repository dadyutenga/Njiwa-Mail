import { AuthLayout } from "@/components/layout/AuthLayout";
import { AuthForm } from "@/features/auth/AuthForm";

export default function AuthPage({ mode }: { mode: string }) {
  return (
    <AuthLayout>
      <AuthForm mode={mode} />
    </AuthLayout>
  );
}
