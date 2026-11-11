import { useState } from "react";
import { Link } from "react-router-dom";
import { DataTable, type Column } from "@/components/shared/DataTable";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/features/i18n/i18n";
import type { RecordItem } from "@/types";
import { initialRecords } from "./mock-records";
import { RecordFormModal } from "./RecordFormModal";

const columns: Column<RecordItem>[] = [
  {
    key: "name",
    label: "Name",
    render: (row) => (
      <Link className="font-medium text-primary hover:underline" to={`/records/${row.id}`}>
        {row.name}
      </Link>
    ),
  },
  {
    key: "status",
    label: "Status",
    render: (row) => (
      <Badge variant={row.status === "Active" ? "default" : "secondary"}>{row.status}</Badge>
    ),
  },
  { key: "owner", label: "Owner" },
];

export function RecordsView() {
  const [rows, setRows] = useState<RecordItem[]>(initialRecords);
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState("All");
  const [editing, setEditing] = useState<RecordItem | null>(null);
  const { t } = useI18n();

  const handleEdit = (row: RecordItem | null) => {
    setEditing(row);
    setOpen(true);
  };

  const handleSave = (item: RecordItem) => {
    setRows((prev) =>
      editing ? prev.map((x) => (x.id === editing.id ? item : x)) : [...prev, item],
    );
  };

  const filteredRows = status === "All" ? rows : rows.filter((row) => row.status === status);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t.nav.records}</h1>
          <p className="mt-2 text-muted-foreground">Manage example records stored in memory.</p>
        </div>
        <Button onClick={() => handleEdit(null)}>Create record</Button>
      </div>

      <div className="flex items-center gap-3">
        <Label htmlFor="status-filter">Status</Label>
        <select
          id="status-filter"
          className="rounded-md border bg-background px-3 py-2 text-sm"
          value={status}
          onChange={(event) => setStatus(event.target.value)}
        >
          <option value="All">All</option>
          <option value="Active">Active</option>
          <option value="Draft">Draft</option>
          <option value="Archived">Archived</option>
        </select>
      </div>

      <DataTable rows={filteredRows} columns={columns} onEdit={handleEdit} />

      <RecordFormModal open={open} onOpenChange={setOpen} record={editing} onSave={handleSave} />
    </div>
  );
}
