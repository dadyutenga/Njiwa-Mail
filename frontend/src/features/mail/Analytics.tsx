import { useQuery } from "@tanstack/react-query";
import { admin } from "@/lib/admin-api";
import { usePhrase } from "@/features/i18n/i18n";
type Row = Record<string, unknown>;
export default function Analytics() {
  const phrase = usePhrase();
  const q = useQuery({
    queryKey: ["analytics"],
    queryFn: () => admin.get<{ items: Row[] }>("/analytics"),
  });
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">{phrase("Analytics")}</h1>
      <p className="text-muted-foreground">
        {phrase("Recorded mail activity from the backend.")}
      </p>
      {q.isPending ? (
        <p role="status">{phrase("Loading analytics…")}</p>
      ) : q.error ? (
        <p role="alert">{q.error.message}</p>
      ) : (
        <div className="overflow-x-auto rounded-xl border bg-card">
          <table className="w-full text-left text-sm">
            <thead>
              <tr>
                {Object.keys(q.data.items[0] || {}).map((key) => (
                  <th className="p-3 capitalize" key={key}>
                    {phrase(key.replaceAll("_", " "))}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {q.data.items.map((row, i) => (
                <tr className="border-t" key={i}>
                  {Object.values(row).map((value, j) => (
                    <td className="p-3" key={j}>
                      {String(value ?? "—")}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          {!q.data.items.length && <p className="p-5">{phrase("No analytics recorded yet.")}</p>}
        </div>
      )}
    </div>
  );
}
