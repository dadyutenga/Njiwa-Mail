import Link from "next/link";
import { REPO_URL } from "@/lib/site";

const cards = [
  { href: "/docs/quickstart", title: "Quickstart", text: "Send a sandbox email from your server in a few minutes." },
  { href: "/docs/api/send-email", title: "API reference", text: "Send, batch, status, cancel, inbound, suppressions." },
  { href: "/docs/webhooks", title: "Webhooks", text: "Signed events, retries, and how to verify them." },
  { href: "/docs/setup/choose-transport", title: "Set up delivery", text: "Managed Postfix on your VPS, or your existing SMTP server." },
];

export default function Home() {
  return (
    <main id="main" className="mx-auto w-full max-w-5xl flex-1 px-4 py-16">
      <p className="text-sm font-semibold uppercase tracking-wide text-fd-muted-foreground">Njiwa-Mail documentation</p>
      <h1 className="mt-3 max-w-3xl text-4xl font-bold tracking-tight sm:text-5xl">Transactional email you run yourself</h1>
      <p className="mt-4 max-w-2xl text-lg text-fd-muted-foreground">
        One small HTTP API for OTPs, password resets and notices, whether your instance sends through its own Postfix or an SMTP server you already have. Written for developers and for coding agents, from the same source.
      </p>
      <div className="mt-6 flex flex-wrap gap-3">
        <Link className="rounded-md bg-fd-primary px-4 py-2 text-sm font-medium text-fd-primary-foreground" href="/docs/quickstart">
          Start with the quickstart
        </Link>
        <Link className="rounded-md border px-4 py-2 text-sm font-medium" href="/docs">
          Browse the docs
        </Link>
        <a className="rounded-md border px-4 py-2 text-sm font-medium" href="/llms.txt">
          llms.txt
        </a>
      </div>
      <ul className="mt-12 grid gap-4 sm:grid-cols-2">
        {cards.map((c) => (
          <li key={c.href} className="rounded-xl border bg-fd-card p-5">
            <Link className="font-semibold hover:underline" href={c.href}>
              {c.title}
            </Link>
            <p className="mt-1 text-sm text-fd-muted-foreground">{c.text}</p>
          </li>
        ))}
      </ul>
      <p className="mt-12 text-sm text-fd-muted-foreground">
        <code>submitted</code> means your mail server accepted the message. It never means delivered. Source:{" "}
        <a className="underline" href={REPO_URL} rel="noopener noreferrer">
          the repository
        </a>
        .
      </p>
    </main>
  );
}
