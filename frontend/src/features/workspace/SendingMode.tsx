import { usePhrase } from "@/features/i18n/i18n";
import type { Env } from "./context";
import { useSearchParams } from "react-router-dom";
import { Field, selectClass } from "./ui";

/** Mode on a sending page is local to that URL; navigation does not inherit it. */
export function useSendingModeParam() {
  const [params, setParams] = useSearchParams();
  const mode: Env = params.get("mode") === "sandbox" ? "sandbox" : "live";
  const setMode = (value: Env) =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (value === "sandbox") next.set("mode", value);
        else next.delete("mode");
        next.delete("offset");
        return next;
      },
      { replace: true },
    );
  return [mode, setMode] as const;
}

export function withSendingMode(path: string, mode: Env) {
  const url = new URL(path, "http://workspace.local");
  if (mode === "sandbox") url.searchParams.set("mode", mode);
  return url.pathname + url.search + url.hash;
}

export function SendingMode({
  value,
  onChange,
  disabled = false,
}: {
  value: Env;
  onChange: (value: Env) => void;
  disabled?: boolean;
}) {
  const phrase = usePhrase();
  return (
    <Field label={phrase("Sending mode")}>
      <select
        className={selectClass}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value === "sandbox" ? "sandbox" : "live")}
      >
        <option value="live">{phrase("Live — send to real recipients")}</option>
        <option value="sandbox">{phrase("Sandbox — capture only")}</option>
      </select>
    </Field>
  );
}
