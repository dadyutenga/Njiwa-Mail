import { usePhrase } from "@/features/i18n/i18n";
import type { DeliveryMode } from "./Installation";

/** Exactly two delivery modes, as an accessible radio group. */
export function DeliveryModeCard({
  value,
  onChange,
  disabled,
  recommended,
  managedBlocked,
}: {
  value: DeliveryMode | "";
  onChange: (m: DeliveryMode) => void;
  disabled?: boolean;
  recommended?: DeliveryMode;
  managedBlocked?: boolean;
}) {
  const phrase = usePhrase();
  const options: { mode: DeliveryMode; title: string; body: string }[] = [
    {
      mode: "managed_mta",
      title: "Send from my VPS",
      body: "Njiwa-Mail helps configure a local mail delivery service on this server. Needs outbound port 25, a public IP, DNS and PTR (reverse DNS) access.",
    },
    {
      mode: "existing_smtp",
      title: "Connect an existing mail server",
      body: "Use a mail server you already run, such as Postfix, Exim or Mailcow, over authenticated SMTP. No new mail server is installed.",
    },
  ];
  return (
    <fieldset className="grid gap-3 sm:grid-cols-2" disabled={disabled}>
      <legend className="mb-2 text-sm font-semibold">{phrase("How will you send email?")}</legend>
      {options.map((o) => (
        <label
          key={o.mode}
          className={
            "flex cursor-pointer gap-3 rounded-xl border bg-card p-4 transition focus-within:ring-2 focus-within:ring-ring " +
            (value === o.mode ? "border-primary" : "hover:border-primary/60")
          }
        >
          <input
            type="radio"
            name="delivery-mode"
            value={o.mode}
            checked={value === o.mode}
            onChange={() => onChange(o.mode)}
            className="mt-1"
          />
          <span>
            <span className="block font-semibold">
              {phrase(o.title)}
              {recommended === o.mode && (
                <span className="ml-2 rounded bg-success/15 px-1.5 text-xs font-medium text-success">
                  {phrase("Recommended for this server")}
                </span>
              )}
            </span>
            {o.mode === "managed_mta" && managedBlocked && (
              <span className="mt-1 block text-xs text-destructive">
                {phrase(
                  "Not possible on this server: it already runs a mail service or cannot run one. The installer will refuse and change nothing.",
                )}
              </span>
            )}
            <span className="mt-1 block text-sm text-muted-foreground">{phrase(o.body)}</span>
          </span>
        </label>
      ))}
    </fieldset>
  );
}
