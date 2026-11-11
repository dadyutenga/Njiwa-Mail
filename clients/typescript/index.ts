// Typed client for the Njiwa Mail /v1 API.

export interface EmailRequest {
  from: string;
  to: string;
  template?: string;
  template_version?: number;
  locale?: "en" | "sw";
  variables?: Record<string, string | number>;
  subject?: string;
  html?: string;
  text?: string;
  reply_to?: string;
  cc?: string[];
  bcc?: string[];
  priority?: "high" | "normal";
  send_at?: string;
}

export interface Accepted {
  id: string;
  status: string;
}

export class NjiwaMail {
  constructor(private readonly baseURL: string, private readonly apiKey: string) {}

  async send(idempotencyKey: string, email: EmailRequest): Promise<Accepted> {
    return this.request("POST", "/v1/emails", idempotencyKey, email);
  }

  async cancel(id: string): Promise<void> {
    await this.request("POST", `/v1/emails/${id}/cancel`);
  }

  async status(id: string): Promise<Accepted> {
    return this.request("GET", `/v1/emails/${id}`);
  }

  private async request(method: string, path: string, idempotencyKey?: string, body?: unknown): Promise<Accepted> {
    const headers: Record<string, string> = { Authorization: `Bearer ${this.apiKey}`, Accept: "application/json" };
    if (body) headers["Content-Type"] = "application/json";
    if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
    const response = await fetch(this.baseURL.replace(/\/$/, "") + path, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
    });
    if (response.status >= 500 || response.status === 429) {
      throw new Error(`retryable ${response.status}`);
    }
    if (!response.ok) {
      throw new Error(`njiwa-mail ${response.status}`);
    }
    return response.json() as Promise<Accepted>;
  }
}

// verifyWebhook checks X-DM-Signature the same way as the Go client: HMAC-SHA256 of "t.body".
export async function verifyWebhook(secret: string, header: string, body: string, nowSeconds: number): Promise<boolean> {
  const parts = Object.fromEntries(header.split(",").map((part) => {
    const [key, value] = part.split("=");
    return [key, value];
  }));
  const timestamp = Number(parts.t);
  if (!parts.v1 || !Number.isFinite(timestamp) || Math.abs(nowSeconds - timestamp) > 300) return false;
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const mac = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(`${parts.t}.${body}`));
  const hex = [...new Uint8Array(mac)].map((b) => b.toString(16).padStart(2, "0")).join("");
  if (hex.length !== parts.v1.length) return false;
  let diff = 0;
  for (let i = 0; i < hex.length; i++) diff |= hex.charCodeAt(i) ^ parts.v1.charCodeAt(i);
  return diff === 0;
}
