import { useState, type FormEvent } from "react";
import { admin } from "@/lib/admin-api";
export default function Privacy() {
  const [project, setProject] = useState("");
  const [mode, setMode] = useState("sandbox");
  const [email, setEmail] = useState("");
  const [confirmEmail, setConfirmEmail] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const data = { project, mode, email };
  const exportData = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const result = await admin.send<unknown>("/privacy/export", "POST", data);
      const blob = new Blob([JSON.stringify(result, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "njiwa-mail-recipient-export.json";
      a.click();
      URL.revokeObjectURL(url);
      setMessage("Export downloaded.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Export failed");
    }
  };
  const deleteData = async () => {
    setError("");
    if (confirmEmail !== email) {
      setError("Enter the same email to confirm deletion.");
      return;
    }
    if (!window.confirm(`Permanently delete stored data for ${email} in ${project}/${mode}?`))
      return;
    try {
      await admin.send("/privacy/delete", "POST", data);
      setMessage("Recipient data deleted.");
      setConfirmEmail("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Deletion failed");
    }
  };
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">Recipient privacy</h1>
      <p className="text-muted-foreground">
        Export or remove one recipient’s stored data in a specific project and environment.
      </p>
      <form onSubmit={exportData} className="grid max-w-xl gap-4 rounded-xl border bg-card p-5">
        <label className="grid gap-1 text-sm">
          Project
          <input
            className="rounded border bg-background p-2"
            value={project}
            onChange={(e) => setProject(e.target.value)}
            required
          />
        </label>
        <label className="grid gap-1 text-sm">
          Environment
          <select
            className="rounded border bg-background p-2"
            value={mode}
            onChange={(e) => setMode(e.target.value)}
          >
            <option value="sandbox">sandbox</option>
            <option value="live">live</option>
          </select>
        </label>
        <label className="grid gap-1 text-sm">
          Recipient email
          <input
            type="email"
            className="rounded border bg-background p-2"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </label>
        <button className="rounded border px-4 py-2">Export data</button>
        <div className="border-t pt-4">
          <label className="grid gap-1 text-sm">
            Re-enter email to confirm deletion
            <input
              type="email"
              className="rounded border bg-background p-2"
              value={confirmEmail}
              onChange={(e) => setConfirmEmail(e.target.value)}
            />
          </label>
          <button
            type="button"
            onClick={deleteData}
            className="mt-3 rounded bg-destructive px-4 py-2 text-destructive-foreground"
          >
            Delete recipient data
          </button>
        </div>
        {error && (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}
        {message && (
          <p role="status" className="text-primary">
            {message}
          </p>
        )}
      </form>
    </div>
  );
}
