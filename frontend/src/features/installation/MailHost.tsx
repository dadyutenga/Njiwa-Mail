import { useQuery } from "@tanstack/react-query";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import {
  CopyButton,
  DataState,
  Empty,
  PageHeader,
  s,
  Section,
  StateBadge,
  Table,
  Time,
  type Row,
} from "@/features/workspace/ui";

// The managed mail host as the installer recorded it: what it did, whether
// delivery evidence is arriving, which domains still need a DKIM key, and where
// a staged DKIM rotation stands. Read-only: the host changes only through the
// installer on the server.

type Source = { last_at: string | null; events_24h: number };
type MailHost = {
  receipts: Row[];
  evidence: { smtp: Source; mta_log: Source; dsn: Source; submitted_24h: number };
  domains_missing_key: string[];
  suggested_commands: string[];
  dkim_rotation: {
    state: string;
    from?: string;
    to?: string;
    ready_to_activate?: boolean;
    dns?: { domain: string; host: string; published: boolean; hint?: string }[];
    activated_at?: string;
    prepared_at?: string;
  };
};

const rotationText: Record<string, string> = {
  none: "No rotation in progress.",
  prepared:
    "Rotation prepared: new keys exist on the host; mail is still signed with the old selector.",
  activated:
    "Rotation activated: mail is signed with the new selector. Retire the old one after about a week.",
  cancelled: "The last rotation was cancelled; signing is unchanged.",
  retired: "The old selector was retired.",
};

export function MailHost() {
  const phrase = usePhrase();
  const q = useQuery({
    queryKey: ["mail-host"],
    queryFn: () => admin.get<MailHost>("/installation/mail-host"),
    refetchInterval: 30_000,
  });
  return (
    <>
      <PageHeader
        title={phrase("Mail host")}
        description={phrase(
          "What the installer did on your mail server, whether delivery evidence is arriving, and the state of DKIM keys. Changes are made with the installer on the server.",
        )}
      />
      <DataState q={q}>
        {(d) => (
          <>
            {d.domains_missing_key.length > 0 && (
              <Section title={phrase("Run the installer again")}>
                <p role="status" className="mb-2 text-sm">
                  {phrase("These verified sending domains have no DKIM key on the mail host yet:")}{" "}
                  <strong>{d.domains_missing_key.join(", ")}</strong>.{" "}
                  {phrase(
                    "Run the installer again so it creates their keys, then publish the DKIM records it prints.",
                  )}
                </p>
                <ol className="space-y-2 text-xs">
                  {d.suggested_commands.map((c) => (
                    <li key={c} className="flex items-start gap-2">
                      <code className="break-all rounded bg-muted p-2">{c}</code>
                      <CopyButton text={c} label="command" />
                    </li>
                  ))}
                </ol>
              </Section>
            )}
            <Section title={phrase("Installer runs")}>
              {d.receipts.length === 0 ? (
                <Empty
                  title={phrase("The installer has not run yet")}
                  description={phrase("Run njiwa-mail installer apply -record on the mail host.")}
                />
              ) : (
                <Table
                  rows={d.receipts}
                  rowKey={(r) => s(r.id)}
                  columns={[
                    {
                      key: "created_at",
                      label: phrase("When"),
                      render: (r) => <Time value={r.created_at} relative />,
                    },
                    { key: "action", label: phrase("Action") },
                    {
                      key: "result_code",
                      label: phrase("Result"),
                      render: (r) => (
                        <StateBadge
                          state={r.result_code === "ok" && r.verified_at ? "pass" : "fail"}
                          label={`${s(r.result_code)}${r.verified_at ? "" : ` · ${phrase("not verified")}`}`}
                        />
                      ),
                    },
                    { key: "applied_version", label: phrase("Postfix") },
                    { key: "approved_by", label: phrase("Approved by") },
                    {
                      key: "plan_digest",
                      label: phrase("Plan"),
                      render: (r) => <code>{s(r.plan_digest)}</code>,
                    },
                    {
                      key: "rollback_state",
                      label: phrase("Rollback"),
                      render: (r) => (r.rollback_state === "none" ? "—" : s(r.rollback_state)),
                    },
                  ]}
                />
              )}
            </Section>
            <Section title={phrase("Delivery evidence")}>
              <p className="mb-2 text-xs text-muted-foreground">
                {phrase(
                  "Evidence comes from the mail server log (njiwa-mail mta-log) and from bounce reports. Silence only means nothing arrived: with no mail sent there are no events.",
                )}
              </p>
              <dl className="grid gap-3 text-sm sm:grid-cols-4">
                {(
                  [
                    ["smtp", "From SMTP replies", d.evidence.smtp],
                    ["mta_log", "From the mail server log", d.evidence.mta_log],
                    ["dsn", "From bounce reports", d.evidence.dsn],
                  ] as const
                ).map(([k, label, src]) => (
                  <div key={k}>
                    <dt className="text-muted-foreground">{phrase(label)}</dt>
                    <dd>
                      {src.last_at ? <Time value={src.last_at} relative /> : phrase("nothing yet")}
                      <span className="block text-xs text-muted-foreground">
                        {src.events_24h} {phrase("in 24 hours")}
                      </span>
                    </dd>
                  </div>
                ))}
                <div>
                  <dt className="text-muted-foreground">{phrase("Handed over in 24 hours")}</dt>
                  <dd>{d.evidence.submitted_24h}</dd>
                </div>
              </dl>
              {d.evidence.submitted_24h > 0 && d.evidence.mta_log.events_24h === 0 && (
                <p role="status" className="mt-2 text-xs">
                  {phrase(
                    "Mail was handed over but no log evidence arrived. If this persists, check that njiwa-mail-mta-log is running.",
                  )}
                </p>
              )}
            </Section>
            <Section title={phrase("DKIM key rotation")}>
              <p className="text-sm">
                {phrase(rotationText[d.dkim_rotation.state] ?? d.dkim_rotation.state)}
              </p>
              {d.dkim_rotation.state === "prepared" && (
                <>
                  <ul className="mt-2 space-y-1 text-xs">
                    {(d.dkim_rotation.dns ?? []).map((c) => (
                      <li key={c.domain}>
                        <StateBadge
                          state={c.published ? "pass" : "fail"}
                          label={c.published ? phrase("published") : phrase("not published")}
                        />{" "}
                        <code>{c.host}</code>
                        {!c.published && c.hint ? ` — ${c.hint}` : ""}
                      </li>
                    ))}
                  </ul>
                  <p role="status" className="mt-2 text-xs">
                    {d.dkim_rotation.ready_to_activate
                      ? phrase(
                          "Every new key is published. You can activate on the host: njiwa-mail installer dkim-rotate activate.",
                        )
                      : phrase(
                          "Publish the new records first; activating earlier would make mail fail DKIM.",
                        )}
                  </p>
                </>
              )}
              <p className="mt-2 text-xs text-muted-foreground">
                {phrase(
                  "Use -record with the dkim-rotate commands so this page shows their state.",
                )}
              </p>
            </Section>
          </>
        )}
      </DataState>
    </>
  );
}
