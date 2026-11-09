let token = "";

export function csrfToken(): string {
  return token;
}

export function setCsrfToken(next: string): void {
  token = next;
}
