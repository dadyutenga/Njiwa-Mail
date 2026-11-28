import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { errorMessage, Field, inputClass, Section } from "@/features/workspace/ui";

type Plan = {
  digest: string;
  summary: string;
  packages: string[] | null;
  blockers: string[] | null;
  warnings: string[] | null;
  conflict: string;
};
type Job = {
  state: "idle" | "running" | "done" | "failed";
  kind?: string;
  lines?: string[] | null;
  error?: string;
  result?: string;
  selector?: string;
  receipt_id?: string;
  recorded?: boolean;
  dkim_records?: Record<string, string> | null;
};

/**
 * Plan, approve and install the managed mail server through the host agent.
 * Nothing runs until the exact plan shown here is approved.
 */
export function HostInstall() {
  const phrase = usePhrase();
  const client = useQueryClient();
  const [hostname, setHostname] = useState("");
  const [bounce, setBounce] = useState("");
  const [plan, setPlan] = useState<Plan | null>(null);
  const [approved, setApproved] = useState(false);
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const job = useQuery({
    queryKey: ["host-job"],
    queryFn: () => admin.get<Job>("/installation/host/job"),
    retry: false,
    refetchInterval: (q) => (q.state.data?.state === "running" ? 2000 : false),
  });
  const running = job.data?.state === "running";
  const body = () => ({ hostname: hostname.trim(), bounce_domain: bounce.trim() });
  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const blocked = (plan?.blockers?.length ?? 0) > 0 || !!plan?.conflict;
  return (
    <Section title={phrase("Set up this mail host")}>
      <p className="mb-3 text-sm text-muted-foreground">
        {phrase(
          "The host agent shows what it would change, and installs only after you approve that exact plan. It backs up first, rolls back on failure, and refuses a host that already runs a mail server.",
        )}
      </p>
      <form
        className="grid gap-3 sm:grid-cols-2"
        onSubmit={(e) => {
          e.preventDefault();
          void act(async () => {
            setApproved(false);
            setPlan(await admin.send<Plan>("/installation/host/plan", "POST", body()));
          });
        }}
      >
        <Field
          label={phrase("Public mail hostname")}
          hint={phrase("Leave empty to use MAIL_HOSTNAME.")}
        >
          <input
            className={inputClass}
            value={hostname}
            onChange={(e) => {
              setHostname(e.target.value);
              setPlan(null);
            }}
          />
        </Field>
        <Field
          label={phrase("Bounce domain")}
          hint={phrase("Leave empty to use the domain of BOUNCE_ADDRESS.")}
        >
          <input
            className={inputClass}
            value={bounce}
            onChange={(e) => {
              setBounce(e.target.value);
              setPlan(null);
            }}
          />
        </Field>
        <div>
          <Button type="submit" variant="outline" disabled={busy || running}>
            {phrase("Plan")}
          </Button>
        </div>
      </form>
      {error && (
        <p role="alert" className="mt-3 text-sm text-destructive">
          {error}
        </p>
      )}
      {plan && (
        <div className="mt-4 space-y-3" data-testid="host-plan">
          <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded bg-muted p-3 text-xs">
            {plan.summary}
          </pre>
          {(plan.warnings ?? []).map((w) => (
            <p key={w} className="text-sm text-warning">
              {w}
            </p>
          ))}
          {blocked ? (
            <div
              role="alert"
              className="rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm"
            >
              {(plan.blockers ?? []).concat(plan.conflict ? [plan.conflict] : []).map((b) => (
                <p key={b}>{b}</p>
              ))}
              <p className="mt-2 font-medium">
                {phrase(
                  "Connect the mail server that is already here as an existing mail server. Njiwa-Mail never replaces it.",
                )}
              </p>
            </div>
          ) : (
            <>
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-1"
                  checked={approved}
                  onChange={(e) => setApproved(e.target.checked)}
                />
                <span>
                  {phrase("I reviewed the plan with digest")} <code>{plan.digest}</code>
                </span>
              </label>
              <Button
                disabled={!approved || busy || running}
                onClick={() =>
                  act(async () => {
                    await admin.send("/installation/host/apply", "POST", {
                      ...body(),
                      digest: plan.digest,
                    });
                    setPlan(null);
                    await client.invalidateQueries({ queryKey: ["host-job"] });
                  })
                }
              >
                {phrase("Approve and install")}
              </Button>
            </>
          )}
        </div>
      )}
      {job.data?.state && job.data.state !== "idle" && (
        <div className="mt-4" role="status" data-testid="host-job">
          <p className="text-sm font-medium">
            {phrase(job.data.kind === "rollback" ? "Roll back" : "Installation")}:{" "}
            {phrase(
              { running: "Running", done: "Finished", failed: "Failed", idle: "" }[job.data.state],
            )}
          </p>
          {job.data.error && <p className="text-sm text-destructive">{job.data.error}</p>}
          <pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap rounded bg-muted p-3 text-xs">
            {(job.data.lines ?? []).join("\n")}
          </pre>
          {job.data.state === "done" && job.data.dkim_records && job.data.kind === "apply" && (
            <div className="mt-2 text-sm">
              <p>{phrase("Publish these DKIM records, then check each domain on its page.")}</p>
              <ul className="mt-1 space-y-1">
                {Object.entries(job.data.dkim_records).map(([domain, txt]) => (
                  <li key={domain} className="break-all">
                    <code>
                      {job.data!.selector}._domainkey.{domain} TXT &quot;{txt}&quot;
                    </code>
                  </li>
                ))}
              </ul>
              <p className="mt-2">{phrase("Then run the connection test and activate.")}</p>
            </div>
          )}
        </div>
      )}
      <details className="mt-4 text-sm">
        <summary className="cursor-pointer">{phrase("Roll back the last installation")}</summary>
        <div className="mt-2 flex flex-wrap items-end gap-2">
          <Field label={phrase("Type ROLL BACK to confirm")}>
            <input
              className={inputClass}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </Field>
          <Button
            variant="destructive"
            disabled={confirm !== "ROLL BACK" || busy || running}
            onClick={() =>
              act(async () => {
                await admin.send("/installation/host/rollback", "POST", {
                  hostname: hostname.trim(),
                  confirm,
                });
                setConfirm("");
                await client.invalidateQueries({ queryKey: ["host-job"] });
              })
            }
          >
            {phrase("Roll back")}
          </Button>
        </div>
      </details>
    </Section>
  );
}
