export const APP_CONFIG = {
  name: "Ditronics Mail",
  shortName: "Mail",
  version: "1.0.0",
  defaultLanguage: "en",
  defaultTheme: "system",
  defaultPageSize: 10,
  pageSizeOptions: [5, 10, 20, 50, 100],
} as const;

export const STORAGE_KEYS = {
  auth_token: "template_auth_token",
  auth_user: "template_auth_user",
  theme: "template_theme",
  language: "template_language",
  sidebar_collapsed: "template_sidebar_collapsed",
} as const;
