import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Circle, CircleDot, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { useWorkspace } from "@/features/workspace/context";
import { errorMessage, Section } from "@/features/workspace/ui";

type Step = { id: string; state: "done" | "current" | "todo" | "blocked"; detail: string };
type Flow = { steps?: Step[]; profile_id?: string };

const titles: Record<string, string> = {
  scan: "Scan this server",
  choose: "Choose how mail is sent",
  install: "Plan, approve and install",
  dns: "Publish the DNS records",
  test: "Test the connection",
  activate: "Activate",
};

const stateText = { done: "Done", current: "Next", todo: "Later", blocked: "Blocked" } as const;

function Icon({ state }: { state: Step["state"] }) {
  if (state === "done") return <CheckCircle2 size={18} className="text-success" aria-hidden />;
  if (state === "current") return <CircleDot size={18} className="text-info" aria-hidden />;
  if (state === "blocked") return <XCircle size={18} className="text-warning" aria-hidden />;
  return <Circle size={18} className="text-muted-foreground" aria-hidden />;
}

/**
 * The whole setup as one list: scan, choose, install, DNS, test, activate. The
 * server derives each step from what is recorded, so it resumes where it was.
 */
export function HostFlow() {
  const phrase = usePhrase();
  const { to } = useWorkspace();
  const client = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const q = useQuery({
    queryKey: ["host-flow"],
    queryFn: () => admin.get<Flow>("/installation/host/flow"),
    refetchInterval: 10_000,
  });
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
      await client.invalidateQueries({ queryKey: ["host-flow"] });
    }
  };
  const steps = q.data?.steps ?? [];
  if (steps.length === 0) return null;
  const profile = q.data?.profile_id;
  const action = (id: string) => {
    const button = (label: string, fn: () => Promise<unknown>) => (
      <Button size="sm" disabled={busy} onClick={() => void run(fn)}>
        {phrase(label)}
      </Button>
    );
    switch (id) {
      case "scan":
        return button("Scan now", () => admin.send("/installation/discovery", "POST"));
      case "choose":
        return button("Send from my VPS", () =>
          admin.send("/installation/wizard", "PUT", { selected_mode: "managed_mta" }),
        );
      case "install":
        return (
          <a className="text-sm font-medium text-primary underline" href="#host-install">
            {phrase("Go to the installer")}
          </a>
        );
      case "dns":
        return (
          <Link className="text-sm font-medium text-primary underline" to={to("domains")}>
            {phrase("Open Domains")}
          </Link>
        );
      case "test":
        return profile
          ? button("Run connection test", () =>
              admin.send(`/installation/transport-profiles/${profile}/test`, "POST"),
            )
          : null;
      case "activate":
        return profile
          ? button("Activate", () =>
              admin.send(`/installation/transport-profiles/${profile}/activate`, "POST", {}),
            )
          : null;
      default:
        return null;
    }
  };
  return (
    <Section title={phrase("Setup progress")}>
      <ol className="space-y-2">
        {steps.map((st) => (
          <li
            key={st.id}
            className="flex items-start gap-3 rounded-lg border p-3"
            data-flow={st.id}
          >
            <Icon state={st.state} />
            <div className="min-w-0 flex-1 text-sm">
              <p className="font-medium">
                {phrase(titles[st.id] ?? st.id)}{" "}
                <span className="text-xs font-normal text-muted-foreground">
                  — {phrase(stateText[st.state])}
                </span>
              </p>
              {st.state !== "done" && <p className="text-xs">{phrase(st.detail)}</p>}
              {st.state === "blocked" && st.id === "choose" && (
                <Link className="text-sm font-medium text-primary underline" to={to("mail-server")}>
                  {phrase("Connect the existing mail server")}
                </Link>
              )}
              {st.state === "current" && <div className="mt-2">{action(st.id)}</div>}
            </div>
          </li>
        ))}
      </ol>
      {error && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {error}
        </p>
      )}
    </Section>
  );
}
