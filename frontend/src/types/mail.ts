export interface ConsoleMe {
  id: string;
  name: string;
  role: "admin";
}

export interface ConsoleWorker {
  healthy: boolean;
  last_seen?: string;
}

export interface ConsoleOverview {
  database: string;
  smtp_configured: boolean;
  worker: ConsoleWorker;
  projects: number;
  messages: number;
  counts: Record<string, number>;
}

export interface MailMessage {
  id: string;
  state: string;
  template: string;
  template_version: number;
  project: string;
  mode: string;
  attempts: number;
  created_at: string;
  updated_at: string;
  expires_at: string;
  submitted_at?: string;
  completed_at?: string;
  idempotency_key: string;
  smtp_reply?: string;
  metadata: Record<string, string>;
  events?: MailEvent[];
}

export interface MailEvent {
  type: string;
  reason?: string;
  at: string;
}

export interface ConsoleEnvironment {
  mode: string;
  enabled: boolean;
  restrict_recipients: boolean;
  origin_count: number;
  allowlist_count: number;
  sender_count: number;
}

export interface ConsoleProject {
  name: string;
  enabled: boolean;
  send_per_minute: number;
  send_per_day: number;
  recipient_per_minute: number;
  recipient_per_hour: number;
  environments: ConsoleEnvironment[];
}

export interface ConsoleKey {
  prefix: string;
  project: string;
  mode: string;
  label: string;
  scopes: string[];
  status: string;
  expires_at?: string;
}
