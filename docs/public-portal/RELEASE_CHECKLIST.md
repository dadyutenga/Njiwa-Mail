# Documentation release checklist

Automated checks cannot do these. Do them before announcing a docs release, and record the date in `docs/RELEASE_REPORT.md`.

- [ ] **"Open in …" URL formats.** In a real browser, on the deployed site, open one page and use each item of **Open ▾**. Confirm ChatGPT, Scira AI and T3 Chat open with the prompt filled in (not sent), and that Claude gets the prompt on the clipboard. If a format stopped working, change it to `mode: "copy"` in `documentation/lib/ai-services.ts` and update its `evidence` and `checkedOn`. The formats come from community and reference-implementation documentation, not from the providers.
- [ ] **A cold read.** Someone who has not seen the project follows the quickstart against a sandbox instance using only the site, and sends one sandbox email.
- [ ] **An agent run.** Give a coding agent only `/llms.txt`, the `.md` pages, `/openapi.yaml` and the skill, ask for an OTP integration, and check the generated code compiles and follows the rules (no key in client code, idempotency key per event, no delivery claim).
- [ ] **Screen reader and phone** pass on the landing page, a docs page and search.
- [ ] **Managed Postfix and Cloudflare one-click** pages still say what is verified. Update them when a real-host run happens.
- [ ] **Kiswahili** (dashboard strings only): native-speaker review.
- [ ] **Deploy and rollback** rehearsed on the real docs host with the previous tag.
