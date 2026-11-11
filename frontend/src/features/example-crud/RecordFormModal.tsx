import { useState, useEffect, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { RecordItem } from "@/types";

interface RecordFormModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  record: RecordItem | null;
  onSave: (record: RecordItem) => void;
}

export function RecordFormModal({ open, onOpenChange, record, onSave }: RecordFormModalProps) {
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [status, setStatus] = useState<"Active" | "Draft" | "Archived">("Active");

  useEffect(() => {
    if (record) {
      setName(record.name);
      setOwner(record.owner);
      setStatus(record.status);
    } else {
      setName("");
      setOwner("");
      setStatus("Draft");
    }
  }, [record, open]);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    onSave({
      id: record?.id ?? crypto.randomUUID(),
      name,
      owner,
      status,
      createdAt: record?.createdAt ?? new Date().toISOString().split("T")[0],
    });
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{record ? "Edit record" : "Create record"}</DialogTitle>
          <DialogDescription>Changes are stored in demo memory.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="record-name">Name</Label>
            <Input
              id="record-name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Record name"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="record-owner">Owner</Label>
            <Input
              id="record-owner"
              required
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
              placeholder="Record owner"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="record-status">Status</Label>
            <select
              id="record-status"
              className="w-full rounded-md border bg-background px-3 py-2 text-sm"
              value={status}
              onChange={(e) => setStatus(e.target.value as "Active" | "Draft" | "Archived")}
            >
              <option value="Active">Active</option>
              <option value="Draft">Draft</option>
              <option value="Archived">Archived</option>
            </select>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
