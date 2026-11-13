// Public, non-secret links. Empty values hide their associated links.
// VITE_DOCS_URL is the origin of the public documentation site (documentation/, see docs/public-portal/DEPLOY.md);
// there is no central hosted docs site, so it is empty unless the operator publishes one.
export const welcomeLinks = {
  github: import.meta.env.VITE_GITHUB_URL ?? "https://github.com/dadyutenga/Njiwa-Mail",
  docs: import.meta.env.VITE_DOCS_URL ?? "",
};
