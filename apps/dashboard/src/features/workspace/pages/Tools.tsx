import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import { useSendingEnvironment, useWorkspace } from "../context";
import { SendingMode, useSendingModeParam } from "../SendingMode";
import {
  CopyButton,
  DataState,
  errorMessage,
  Field,
  PageHeader,
  run,
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

type RelayState = "not_enabled" | "starting" | "running" | "stale" | "error";

const relayStates: Record<RelayState, { label: string; badge: string; next: string }> = {
  not_enabled: {
    label: "Not enabled",
    badge: "cancelled",
    next: "Set COMPOSE_PROFILES=relay and RELAY_PUBLIC_HOST in .env, put fullchain.pem and privkey.pem in the relay-tls directory, then run: docker compose up -d",
  },
  starting: { label: "Starting", badge: "pending", next: "Wait a few seconds." },
  running: {
    label: "Running",
    badge: "active",
    next: "Use the test below to check the connection, sign-in and queueing.",
  },
  stale: {
    label: "Not reporting",
    badge: "failed",
    next: "The relay has not reported in for over a minute. Check docker compose logs relay, then run: docker compose --profile relay up -d relay",
  },
  error: {
    label: "Error",
    badge: "failed",
    next: "Fix the problem below. The relay retries by itself.",
  },
};

type RelayStep = { key: string; title: string; ok: boolean; skipped?: boolean; detail: string };
type RelayTestResult = {
  ok: boolean;
  address: string;
  tls_name: string;
  message_id: string;
  steps: RelayStep[];
};

/** Opens a real SMTP session to the relay and shows each step. */
function RelayTest() {
  const phrase = usePhrase();
  const { project, to } = useWorkspace();
  const [result, setResult] = useState<RelayTestResult | null>(null);
  const [busy, setBusy] = useState(false);
  return (
    <Section className="mt-4" title={phrase("Send a test through the relay")}>
      <p className="mb-3 text-sm text-muted-foreground">
        {phrase(
          "Signs in with a temporary API key and sends one Sandbox message from this project's default sender to itself. No external mail is sent.",
        )}
      </p>
      <Button
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          setResult(null);
          const r = await run(
            () => admin.send<RelayTestResult>(`/w/${project}/sandbox/relay-test`, "POST"),
            "",
          );
          setBusy(false);
          if (r) setResult(r);
        }}
      >
        {busy ? phrase("Testing…") : phrase("Send a test through the relay")}
      </Button>
      {result && (
        <div className="mt-3" role="status" data-testid="relay-test">
          <p className="text-sm font-medium">
            {phrase(result.ok ? "The relay test passed" : "The relay test failed")}
          </p>
          <ol className="mt-2 space-y-2 text-sm">
            {result.steps.map((st) => (
              <li key={st.key} className="flex items-start gap-2" data-relay-step={st.key}>
                <StateBadge
                  state={st.ok ? "active" : st.skipped ? "cancelled" : "failed"}
                  label={phrase(st.ok ? "passed" : st.skipped ? "skipped" : "failed")}
                />
                <span className="min-w-0">
                  <span className="font-medium">{phrase(st.title)}</span>
                  <span className="block break-words text-xs text-muted-foreground">
                    {st.detail}
                  </span>
                </span>
              </li>
            ))}
          </ol>
          {result.message_id && (
            <p className="mt-2 text-sm">
              <Link
                className="text-primary underline"
                to={to(`messages/${result.message_id}?mode=sandbox`)}
              >
                {phrase("view the message")}
              </Link>
            </p>
          )}
          <p className="mt-2 text-xs text-muted-foreground">
            {phrase(
              "This test connects from the Njiwa-Mail API to the relay. It does not prove that your application can reach the public host and port.",
            )}{" "}
            <code>{result.address}</code>
          </p>
        </div>
      )}
    </Section>
  );
}

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
          const state = relayStates[s(d.state) as RelayState] ?? relayStates.stale;
          const settings = [
            `Host: ${s(d.host)}`,
            `Port: ${s(d.port)}`,
            `Security: ${s(d.tls) === "implicit" ? "TLS on connect (SMTPS)" : "STARTTLS"}`,
            "Authentication: PLAIN",
            "Username: anything",
            "Password: <your API key with emails:send_raw>",
          ].join("\n");
          return (
            <>
              <Section>
                <div className="flex flex-wrap items-center gap-3" data-relay-state={s(d.state)}>
                  <StateBadge state={state.badge} label={phrase(state.label)} />
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
                {Boolean(d.detail) && (
                  <p className="mt-3 break-words text-sm text-destructive">{s(d.detail)}</p>
                )}
                <p className="mt-3 text-sm text-muted-foreground">{phrase(state.next)}</p>
                {Boolean(d.cert_not_after) && (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {phrase("Certificate valid until")}: <Time value={d.cert_not_after} />
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
                {d.host_source === "request" && (
                  <p className="mt-3 text-xs text-muted-foreground">
                    {phrase(
                      "The host is the address you opened this page on. Set RELAY_PUBLIC_HOST if applications connect to another name.",
                    )}
                  </p>
                )}
                <p className="mt-2 text-xs text-muted-foreground">
                  {phrase(
                    "“Running” means the relay reported in the last minute that it is listening with a certificate. It does not prove the port is reachable from your application's network. Mail it accepts is queued like any other message; delivery is shown in the message timeline.",
                  )}
                </p>
              </Section>
              <RelayTest />
            </>
          );
        }}
      </DataState>
    </>
  );
}
