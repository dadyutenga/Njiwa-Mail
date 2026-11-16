/**
 * Build-time site facts. There is no central hosted Njiwa-Mail endpoint: every
 * example uses the placeholder below, and the operator sets the docs origin.
 */
export const DOCS_BASE_URL = (process.env.NEXT_PUBLIC_DOCS_BASE_URL ?? "https://docs.example.com").replace(/\/$/, "");
export const API_URL_PLACEHOLDER = "https://mail-api.example.com";
export const SOURCE_COMMIT = process.env.NEXT_PUBLIC_DOCS_COMMIT ?? "dev";
export const DOCS_VERSION = "1.0";
export const REPO_URL = "https://github.com/dadyutenga/Njiwa-Mail";
