import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { downloadCsv } from "@/lib/csv";
export type Column<T> = { key: keyof T; label: string; render?: (row: T) => React.ReactNode };
export function DataTable<T extends { id: string }>({
  rows,
  columns,
  onEdit,
}: {
  rows: T[];
  columns: Column<T>[];
  onEdit?: (row: T) => void;
}) {
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<keyof T | undefined>(columns[0]?.key);
  const [descending, setDescending] = useState(false);
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<string[]>([]);
  const [hidden, setHidden] = useState<string[]>([]);
  const filtered = useMemo(
    () =>
      rows
        .filter((row) =>
          columns.some((c) => String(row[c.key]).toLowerCase().includes(search.toLowerCase())),
        )
        .sort(
          (a, b) =>
            String(sort === undefined ? "" : a[sort]).localeCompare(
              String(sort === undefined ? "" : b[sort]),
              undefined,
              { numeric: true },
            ) * (descending ? -1 : 1),
        ),
    [rows, columns, search, sort, descending],
  );
  const maxPage = Math.max(0, Math.ceil(filtered.length / 5) - 1);
  const current = Math.min(page, maxPage);
  const visible = columns.filter((c) => !hidden.includes(String(c.key)));
  function exportCsv() {
    downloadCsv("records.csv", [
      visible.map((c) => c.label),
      ...filtered.map((row) => visible.map((c) => row[c.key])),
    ]);
  }
  return (
    <div className="rounded-2xl border bg-card p-5">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Input
          aria-label="Search records"
          placeholder="Search records…"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(0);
          }}
          className="max-w-xs"
        />
        <details className="relative text-sm">
          <summary className="cursor-pointer rounded-md border px-3 py-2">Columns</summary>
          <div className="absolute z-10 mt-1 min-w-40 space-y-2 rounded-md border bg-popover p-3">
            {columns.map((c) => (
              <label className="flex gap-2" key={String(c.key)}>
                <input
                  type="checkbox"
                  checked={!hidden.includes(String(c.key))}
                  onChange={() =>
                    setHidden((h) =>
                      h.includes(String(c.key))
                        ? h.filter((k) => k !== String(c.key))
                        : [...h, String(c.key)],
                    )
                  }
                />
                {c.label}
              </label>
            ))}
          </div>
        </details>
        <Button variant="outline" onClick={exportCsv}>
          Export CSV
        </Button>
        <span className="ml-auto text-xs text-muted-foreground">{selected.length} selected</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead>
            <tr className="border-b text-muted-foreground">
              <th className="p-3">
                <input
                  aria-label="Select all visible rows"
                  type="checkbox"
                  checked={
                    filtered.slice(current * 5, current * 5 + 5).length > 0 &&
                    filtered
                      .slice(current * 5, current * 5 + 5)
                      .every((r) => selected.includes(r.id))
                  }
                  onChange={(e) => {
                    const ids = filtered.slice(current * 5, current * 5 + 5).map((r) => r.id);
                    setSelected((previous) =>
                      e.target.checked
                        ? [...new Set([...previous, ...ids])]
                        : previous.filter((id) => !ids.includes(id)),
                    );
                  }}
                />
              </th>
              {visible.map((c) => (
                <th
                  className="p-3"
                  key={String(c.key)}
                  scope="col"
                  aria-sort={sort === c.key ? (descending ? "descending" : "ascending") : "none"}
                >
                  <button
                    onClick={() => {
                      if (sort === c.key) setDescending(!descending);
                      else {
                        setSort(c.key);
                        setDescending(false);
                      }
                    }}
                  >
                    {c.label} {sort === c.key ? (descending ? "↓" : "↑") : ""}
                  </button>
                </th>
              ))}
              {onEdit && <th className="p-3">Actions</th>}
            </tr>
          </thead>
          <tbody>
            {filtered.slice(current * 5, current * 5 + 5).map((row) => (
              <tr className="border-b last:border-0" key={row.id}>
                <td className="p-3">
                  <input
                    aria-label={`Select ${row.id}`}
                    type="checkbox"
                    checked={selected.includes(row.id)}
                    onChange={(e) =>
                      setSelected((s) =>
                        e.target.checked ? [...s, row.id] : s.filter((id) => id !== row.id),
                      )
                    }
                  />
                </td>
                {visible.map((c) => (
                  <td className="p-3" key={String(c.key)}>
                    {c.render ? c.render(row) : String(row[c.key])}
                  </td>
                ))}
                {onEdit && (
                  <td>
                    <Button size="sm" variant="ghost" onClick={() => onEdit(row)}>
                      Edit
                    </Button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
        {!filtered.length && (
          <p className="p-8 text-center text-muted-foreground">No matching records.</p>
        )}
      </div>
      <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
        <span>
          {filtered.length} records · Page {current + 1} of {maxPage + 1}
        </span>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={current === 0}
            onClick={() => setPage(current - 1)}
          >
            Previous
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={current === maxPage}
            onClick={() => setPage(current + 1)}
          >
            Next
          </Button>
        </div>
      </div>
    </div>
  );
}
