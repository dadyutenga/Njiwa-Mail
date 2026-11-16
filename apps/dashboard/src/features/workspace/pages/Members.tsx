import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePhrase } from "@/features/i18n/i18n";
import { useWorkspace } from "../context";
import {
  ConfirmButton,
  DataState,
  errorMessage,
  Field,
  inputClass,
  PageHeader,
  run,
  s,
  Section,
  selectClass,
  Table,
  Time,
  type Row,
} from "../ui";

const roles = ["viewer", "developer", "admin", "owner"];

const roleHelp: Record<string, string> = {
  viewer: "Can look at everything in the project but change nothing.",
  developer: "Can also send mail and edit templates, contacts, campaigns and replies.",
  admin:
    "Can also manage API keys, domains, senders, webhooks, settings and the personal-data tools.",
  owner: "Can also manage the members.",
};

export function Members() {
  const phrase = usePhrase();
  const { api, project } = useWorkspace();
  const client = useQueryClient();
  const key = ["ws", project, "members"];
  const q = useQuery({
    queryKey: key,
    queryFn: () => api.get<{ items: Row[]; your_role: string }>("/members"),
  });
  const owner = q.data?.your_role === "owner";
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("developer");
  const [error, setError] = useState("");
  const refresh = () => client.invalidateQueries({ queryKey: key });
  const add = async () => {
    setError("");
    try {
      await api.send("/members", "POST", { email: email.trim(), role });
      setEmail("");
      refresh();
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <>
      <PageHeader
        title={phrase("Members")}
        description={phrase(
          "Only the people listed here, and the operators of this installation, can open this project. Your role decides what you can change.",
        )}
      />
      <DataState q={q}>
        {(d) => (
          <>
            <p className="mb-3 text-sm text-muted-foreground">
              {phrase("Your role")}: <b>{phrase(d.your_role)}</b>
            </p>
            <Table
              rows={d.items}
              rowKey={(r) => s(r.user_id)}
              columns={[
                { key: "email", label: phrase("Person") },
                {
                  key: "role",
                  label: phrase("Role"),
                  render: (r) =>
                    owner ? (
                      <select
                        className={selectClass}
                        aria-label={phrase("Role")}
                        value={s(r.role)}
                        onChange={async (e) => {
                          await run(
                            () =>
                              api.send(`/members/${s(r.user_id)}`, "PATCH", {
                                role: e.target.value,
                              }),
                            phrase("Updated"),
                          );
                          refresh();
                        }}
                      >
                        {roles.map((x) => (
                          <option key={x} value={x}>
                            {phrase(x)}
                          </option>
                        ))}
                      </select>
                    ) : (
                      phrase(s(r.role))
                    ),
                },
                {
                  key: "created_at",
                  label: phrase("Added"),
                  render: (r) => <Time value={r.created_at} relative />,
                },
                {
                  key: "x",
                  label: <span className="sr-only">{phrase("Actions")}</span>,
                  render: (r) =>
                    owner && (
                      <ConfirmButton
                        title={phrase("Remove this member?")}
                        description={phrase("They can no longer open this project.")}
                        confirm="Remove"
                        onConfirm={async () => {
                          await run(
                            () => api.send(`/members/${s(r.user_id)}`, "DELETE"),
                            phrase("Removed"),
                          );
                          refresh();
                        }}
                      >
                        <Trash2 size={14} />
                      </ConfirmButton>
                    ),
                },
              ]}
            />
            {owner && (
              <Section className="mt-4" title={phrase("Add a member")}>
                <div className="grid max-w-xl gap-3">
                  <Field
                    label={phrase("Email address")}
                    hint={phrase(
                      "The person must already have an account. An operator creates accounts.",
                    )}
                  >
                    <input
                      className={inputClass}
                      type="email"
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                    />
                  </Field>
                  <Field label={phrase("Role")} hint={phrase(roleHelp[role])}>
                    <select
                      className={selectClass}
                      value={role}
                      onChange={(e) => setRole(e.target.value)}
                    >
                      {roles.map((x) => (
                        <option key={x} value={x}>
                          {phrase(x)}
                        </option>
                      ))}
                    </select>
                  </Field>
                  {error && (
                    <p role="alert" className="text-sm text-destructive">
                      {error}
                    </p>
                  )}
                  <div>
                    <Button onClick={add} disabled={!email.trim()}>
                      {phrase("Add")}
                    </Button>
                  </div>
                </div>
              </Section>
            )}
          </>
        )}
      </DataState>
    </>
  );
}
