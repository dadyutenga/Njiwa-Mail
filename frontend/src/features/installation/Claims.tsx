import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { usePhrase } from "@/features/i18n/i18n";
import { admin } from "@/lib/admin-api";
import {
  ConfirmButton,
  DataState,
  Empty,
  PageHeader,
  run,
  s,
  Section,
  selectClass,
  StateBadge,
  Time,
} from "@/features/workspace/ui";

// Installation-wide domain ownership (backlog D09). One project may hold a
// domain; a domain verified by several projects before the upgrade waits for
// an operator. Transfers are audited and force the new owner to prove
// ownership again.

type Claim = {
  name: string;
  state: "active" | "review_required";
  project: string | null;
  projects_with_domain: string[] | null;
  claimed_at: string;
  updated_at: string;
};

const claimsKey = ["domain-claims"] as const;

function TransferControl({ c }: { c: Claim }) {
  const phrase = usePhrase();
  const client = useQueryClient();
  const options = (c.projects_with_domain ?? []).filter((p) => p !== c.project);
  const [to, setTo] = useState(options[0] ?? "");
  if (options.length === 0) {
    return (
      <span className="text-xs text-muted-foreground">
        {phrase("No other project has added it")}
      </span>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-2">
      <select
        aria-label={phrase("Transfer to project")}
        className={selectClass + " h-8 w-auto"}
        value={to}
        onChange={(e) => setTo(e.target.value)}
      >
        {options.map((p) => (
          <option key={p} value={p}>
            {p}
          </option>
        ))}
      </select>
      <ConfirmButton
        title={phrase(
          c.state === "review_required" ? "Assign this domain?" : "Transfer this domain?",
        )}
        description={
          <>
            {s(c.name)} {phrase("will belong to")} <strong>{to}</strong>.{" "}
            {phrase(
              "Every project's ownership proof for it is reset: the new owner must check its DNS again, and the previous owner's senders stop being verified at once.",
            )}
          </>
        }
        confirm={phrase(c.state === "review_required" ? "Assign" : "Transfer")}
        disabled={!to}
        onConfirm={async () => {
          await run(
            () => admin.send("/domain-claims/transfer", "POST", { name: c.name, project: to }),
            phrase("Ownership changed. The new owner must run Check now on the domain."),
          );
          client.invalidateQueries({ queryKey: claimsKey });
        }}
      >
        {phrase(c.state === "review_required" ? "Assign" : "Transfer")}
      </ConfirmButton>
    </div>
  );
}

export function DomainClaims() {
  const phrase = usePhrase();
  const q = useQuery({
    queryKey: claimsKey,
    queryFn: () => admin.get<{ items: Claim[] }>("/domain-claims"),
  });
  return (
    <>
      <PageHeader
        title={phrase("Domain ownership")}
        description={phrase(
          "Each sending domain belongs to one project in this installation: the first whose ownership check passed. Other projects cannot send from it until an operator transfers it.",
        )}
      />
      <DataState q={q}>
        {(d) => {
          const items = d.items ?? [];
          const review = items.filter((c) => c.state === "review_required");
          if (items.length === 0)
            return (
              <Empty
                title={phrase("No domains are claimed yet")}
                description={phrase("A domain is claimed when a project's ownership check passes.")}
              />
            );
          return (
            <div className="space-y-4">
              {review.length > 0 && (
                <p
                  role="alert"
                  className="rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm"
                >
                  {review.length}{" "}
                  {phrase(
                    "domain(s) were verified by more than one project before this version. No project can send Live mail from them until you assign each one.",
                  )}
                </p>
              )}
              <Section title={phrase("Claimed domains")}>
                <ul className="divide-y">
                  {items.map((c) => (
                    <li
                      key={c.name}
                      className="flex flex-wrap items-center gap-3 py-3 text-sm"
                      data-claim={c.name}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="font-medium">{c.name}</span>{" "}
                        {c.state === "active" ? (
                          <StateBadge
                            state="pass"
                            label={phrase("Owned by") + " " + s(c.project)}
                          />
                        ) : (
                          <StateBadge state="fail" label={phrase("Needs review")} />
                        )}
                        <span className="block text-xs text-muted-foreground">
                          {phrase("Added by")}: {(c.projects_with_domain ?? []).join(", ") || "—"} ·{" "}
                          {phrase("since")} <Time value={c.claimed_at} relative />
                        </span>
                      </span>
                      <TransferControl c={c} />
                    </li>
                  ))}
                </ul>
              </Section>
            </div>
          );
        }}
      </DataState>
    </>
  );
}
