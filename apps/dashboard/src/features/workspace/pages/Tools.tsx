import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { useSendingEnvironment } from "../context";
import { SendingMode, useSendingModeParam } from "../SendingMode";
import {
  CopyButton,
  DataState,
  errorMessage,
  Field,
  PageHeader,
  s,
  Section,
  StateBadge,
  Table,
  textareaClass,
  Time,
  type Row,
} from "../ui";

// ---------------------------------------------------------------- check addresses

const resultState: Record<string, string> = {
  deliverable: "active",
  risky: "pending",
  undeliverable: "failed",
  unknown: "cancelled",
};

export function CheckAddresses() {
  const phrase = usePhrase();
  const [env, setEnv] = useSendingModeParam();
  const { api } = useSendingEnvironment(env);
  const [text, setText] = useState("");
  const [rows, setRows] = useState<Row[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const addresses = text
    .split(/[\s,;]+/)
    .map((x) => x.trim())
    .filter(Boolean);
  const run = async () => {
    setError("");
    setBusy(true);
    try {
      const r = await api.send<{ results: Row[] }>("/recipients/verify", "POST", {
        emails: addresses,
      });
      setRows(r.results);
    } catch (e) {
      setRows(null);
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <PageHeader
        title={phrase("Check addresses")}
        description={phrase(
          "Find out whether addresses look sendable before you mail them. This never sends mail and never contacts the recipient's mail server.",
        )}
      />
      <div className="mb-4">
        <SendingMode value={env} onChange={setEnv} />
      </div>
      <Section>
        <div className="grid max-w-2xl gap-3">
          <Field
            label={phrase("Addresses")}
            hint={phrase("Separate with spaces, commas or new lines. Up to 100.")}
          >
            <textarea
              className={textareaClass}
              rows={6}
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="name@example.com"
            />
          </Field>
          {addresses.length > 100 && (
            <p role="alert" className="text-sm text-destructive">
              {phrase("Check at most 100 addresses at a time.")}
            </p>
          )}
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div>
            <Button
              onClick={run}
              disabled={busy || addresses.length === 0 || addresses.length > 100}
            >
              {phrase("Check")} {addresses.length > 0 ? `(${addresses.length})` : ""}
            </Button>
          </div>
        </div>
      </Section>
      {rows && (
        <Section className="mt-4" title={phrase("Results")}>
          <Table
            rows={rows}
            rowKey={(r) => s(r.email)}
            columns={[
              { key: "email", label: phrase("Address") },
              {
                key: "result",
                label: phrase("Result"),
                render: (r) => (
                  <StateBadge
                    state={resultState[s(r.result)] ?? "cancelled"}
                    label={phrase(s(r.result))}
                  />
                ),
              },
              {
                key: "reasons",
                label: phrase("Why"),
                render: (r) => {
                  const why = [
                    ...((r.reasons as string[]) ?? []),
                    ...(r.suppressed ? ["suppressed"] : []),
                    ...(r.unsubscribed ? ["unsubscribed"] : []),
                  ];
                  return why.length ? why.join(", ") : "—";
                },
              },
            ]}
          />
          <p className="mt-3 text-xs text-muted-foreground">
            {phrase(
              "“Deliverable” means the domain can receive mail and nothing known blocks it. It is not a promise that the mailbox exists.",
            )}
          </p>
        </Section>
      )}
    </>
  );
}

// ---------------------------------------------------------------- SMTP relay

export function RelayStatus() {
  const phrase = usePhrase();
  const q = useQuery({
    queryKey: ["relay-status"],
    queryFn: () => admin.get<Row>("/relay"),
    refetchInterval: 15_000,
  });
  return (
    <>
      <PageHeader
        title={phrase("SMTP relay")}
        description={phrase(
          "Lets an application that cannot use the HTTP API send through SMTP. The password is an API key with the emails:send_raw scope.",
        )}
      />
      <DataState q={q}>
        {(d) => {
          const host = s(d.host) || "mail.example.com";
          const port = s(d.port) || "587";
          const tls = s(d.tls);
          const settings = [
            `Host: ${host}`,
            `Port: ${port}`,
            `Security: ${tls === "implicit" ? "TLS on connect (SMTPS)" : "STARTTLS"}`,
            "Authentication: PLAIN",
            "Username: anything",
            "Password: <your API key with emails:send_raw>",
          ].join("\n");
          return (
            <>
              <Section>
                <div className="flex flex-wrap items-center gap-3">
                  <StateBadge
                    state={d.running ? "active" : "failed"}
                    label={phrase(d.running ? "Running" : "Not running")}
                  />
                  <span className="text-sm text-muted-foreground">
                    {d.last_seen ? (
                      <>
                        {phrase("Last check-in")} <Time value={d.last_seen} relative />
                      </>
                    ) : (
                      phrase("The relay has not checked in yet.")
                    )}
                  </span>
                </div>
                {!d.running && (
                  <p className="mt-3 text-sm text-muted-foreground">
                    {phrase(
                      "The relay is a separate, optional service. Start it with: docker compose --profile relay up -d relay",
                    )}
                  </p>
                )}
              </Section>
              <Section className="mt-4" title={phrase("Settings for your application")}>
                <pre className="overflow-auto rounded-lg border bg-muted p-3 text-xs">
                  {settings}
                </pre>
                <div className="mt-2 flex justify-end">
                  <CopyButton text={settings} />
                </div>
                <p className="mt-3 text-xs text-muted-foreground">
                  {phrase(
                    "The port is the one the relay listens on. With the Compose profile it is published on host port 2587 (NJIWA_RELAY_PORT), bound to 127.0.0.1, so connect to that port from the same machine.",
                  )}
                </p>
                <p className="mt-2 text-xs text-muted-foreground">
                  {phrase(
                    "“Running” means the relay checked in during the last minute. It does not prove the port is reachable from your application's network. Mail it accepts is queued like any other message; delivery is shown in the message timeline.",
                  )}
                </p>
              </Section>
            </>
          );
        }}
      </DataState>
    </>
  );
}
