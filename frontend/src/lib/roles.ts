export type MailRole = "owner" | "admin";

const rank: Record<MailRole, number> = { admin: 1, owner: 2 };

export function isMailRole(role: string | undefined): role is MailRole {
  return role === "owner" || role === "admin";
}

export function atLeast(role: string | undefined, min: MailRole): boolean {
  if (!isMailRole(role)) return false;
  return rank[role] >= rank[min];
}

export function roleAllowed(role: string | undefined, allowed?: readonly string[]): boolean {
  if (!allowed || allowed.length === 0) return true;
  return Boolean(role && allowed.includes(role));
}
