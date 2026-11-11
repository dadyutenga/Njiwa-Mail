// Public, non-secret links. Empty values hide their associated links.
export const welcomeLinks = {
  github: import.meta.env.VITE_GITHUB_URL ?? "https://github.com/Ditronics-Tz/mail-snd",
  docs:
    import.meta.env.VITE_DOCS_URL ??
    "https://github.com/Ditronics-Tz/mail-snd/blob/main/docs/INTEGRATION.md",
};
