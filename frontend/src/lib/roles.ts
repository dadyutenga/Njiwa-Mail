export type MailRole = "owner" | "admin" | "viewer";

const rank: Record<MailRole, number> = { viewer: 1, admin: 2, owner: 3 };

export function isMailRole(role: string | undefined): role is MailRole {
  return role === "owner" || role === "admin" || role === "viewer";
}

export function atLeast(role: string | undefined, min: MailRole): boolean {
  if (!isMailRole(role)) return false;
  return rank[role] >= rank[min];
}

export function roleAllowed(role: string | undefined, allowed?: readonly string[]): boolean {
  if (!allowed || allowed.length === 0) return true;
  return Boolean(role && allowed.includes(role));
}
