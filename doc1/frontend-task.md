# Njiwa-Mail — Welcome Page: Task Plan and Build Prompt

Date: 11 November 2026
Scope: the public welcome (landing) page of the Njiwa-Mail frontend, with light and dark mode.
Product name on the frontend: **Njiwa-Mail** ("njiwa" is Swahili for dove or pigeon, the bird that carries messages).

Part A is the task plan. Part B is the prompt to give the coding agent; hand it this whole file.

---

## Part A — Task plan

### A1. What the page is for

- First page a visitor sees at `/`, before signing in.
- Explains what Njiwa-Mail is, shows how to send the first email, and leads to **Sign in** and to the **GitHub repository**.
- There is no public signup. The page never shows "Sign up", "Start free", pricing or plans.
- A signed-in user who opens `/` is sent to the dashboard.

### A2. Page sections, in order

| # | Section | Content |
|---|---|---|
| 1 | Navigation bar | Logo and name, links (Features, How it works, Docs, GitHub), theme toggle, Sign in button. Sticky; collapses to a menu on mobile |
| 2 | Hero | Headline, one-sentence description, two buttons (Sign in, View on GitHub), and a code panel showing a real send request |
| 3 | How it works | Diagram of the path: your app → Njiwa-Mail → your mail server → recipient, with replies and bounces coming back |
| 4 | Features | Eight cards (see A3) |
| 5 | Honest status | The message states shown as a timeline, with the line that Njiwa-Mail never claims "delivered" |
| 6 | Developer section | Code panel with tabs (cURL, Go, Laravel, Django, Node) and links to the integration guides |
| 7 | Self-hosted and open source | Runs beside your own mail server with Docker Compose; short quick-start command block; GitHub button |
| 8 | FAQ | Six questions (see A4), accordion |
| 9 | Footer | Name, one-line description, links (Docs, GitHub, Sign in), licence name, "Built in Tanzania" |

### A3. Feature cards (use this copy; do not invent others)

| Title | Text |
|---|---|
| Send over HTTP or SMTP | One API call or your framework's normal mail settings. Both go through the same queue and rules. |
| Never sends twice | Every request carries an idempotency key, so a retry from your app cannot create a second email. |
| Safe retries | Njiwa-Mail retries only when it is certain the mail server did not accept the message. When it cannot tell, it stops and tells you. |
| Receive email too | Replies and inbound mail arrive parsed as JSON, with attachments, through the API or a webhook. |
| Bounces and complaints | Bad addresses and spam reports are picked up and suppressed automatically, so your sender reputation stays clean. |
| Signed webhooks | Your app is told what happened to each message, with signed requests, retries and replay. |
| Templates in English and Kiswahili | Versioned templates with a live preview. Published versions never change under a queued message. |
| Fair queue | Each app takes its turn, so one app's burst cannot delay another app's login codes. |

### A4. FAQ (use this copy)

| Question | Answer |
|---|---|
| What is Njiwa-Mail? | An open-source email platform you host yourself. Your apps send and receive email through one API, using your own mail server. |
| Do I need to pay per email? | No. You run it on your own server, so there is no per-message fee. |
| Does it replace my mail server? | No. It sits beside a mail server such as Mailcow, which still does the actual delivery and DKIM signing. |
| Can I send newsletters? | No. Njiwa-Mail is for transactional email: login codes, password resets, receipts and notices. |
| How do I know an email arrived? | Njiwa-Mail shows what it can prove: that your mail server accepted the message, and whether a bounce or complaint came back. It does not claim inbox placement or that someone read it. |
| Can I test without sending real email? | Yes. Sandbox keys capture messages locally and never contact a real mail server. |

### A5. Hero copy

- Headline: **Your own mail carrier.**
- Description: Njiwa-Mail is an open-source email platform for your apps. Send, receive and track email through one API, on servers you control.
- Buttons: **Sign in** (primary), **View on GitHub** (secondary).

### A6. Design direction

The idea: airmail and the carrier pigeon. Warm paper in light mode, night sky in dark mode. It should look like a product someone designed, not a template.

Colour tokens (starting values; the agent must check contrast and adjust if any pair fails):

| Token | Light | Dark |
|---|---|---|
| `--bg` | `#FAF7F2` | `#0F1114` |
| `--surface` | `#FFFFFF` | `#171A1F` |
| `--text` | `#1B1A17` | `#ECE8E1` |
| `--text-muted` | `#6B675F` | `#9A968E` |
| `--line` | `#E4DED3` | `#2A2E35` |
| `--accent` (airmail blue) | `#2F5BEA` | `#7C9BFF` |
| `--accent-2` (stamp red) | `#D9432F` | `#FF7A66` |
| `--code-bg` | `#14161A` | `#0A0C0F` |

Rules:

- One accent does the work (blue). Red is for small marks only: the stamp, the airmail stripe, error states.
- Typography: a display face with character for headings, a clean sans for body, a monospace for code. Load at most three font families, self-hosted or from one font provider, with `font-display: swap`.
- Signature details: a thin red-and-blue airmail stripe used once or twice (top of the page, edge of the code panel); a postmark-style circular stamp near the hero; a simple original dove mark for the logo, drawn as SVG.
- The hero code panel looks like a real terminal or editor card, with a copy button and a small response block showing `202` and `"status": "queued"`.
- Motion: small and purposeful. A fade-and-rise on section entry, and the path in the "How it works" diagram drawing itself once. Respect `prefers-reduced-motion`.
- No stock photos, no generic 3D blobs, no purple-gradient hero, no emoji as icons. Icons come from one consistent set, or are drawn as SVG.
- Generous spacing, a clear grid, and one idea per section.

### A7. Light and dark mode

- [ ] Follows the system setting on first visit.
- [ ] A toggle in the navigation bar switches theme; the choice is saved and restored on the next visit.
- [ ] The theme is applied before first paint, so there is no flash of the wrong theme on load.
- [ ] Every colour comes from the tokens above. No hard-coded colours in components.
- [ ] Both themes are designed, not inverted: check every section, the code panel, the diagram, focus rings, hover states and the logo in both.
- [ ] The toggle has an accessible label and shows the current state.

### A8. Truthfulness rules

- [ ] No invented testimonials, customer logos, user counts, uptime figures or benchmarks.
- [ ] No "delivered" or "guaranteed inbox" wording anywhere.
- [ ] No pricing, plans, "free trial" or signup.
- [ ] Code samples use placeholder hosts and keys (`https://mail.example.com`, `NJIWA_API_KEY`) and match the real API: `POST /v1/emails` with `Authorization: Bearer` and `Idempotency-Key`.
- [ ] The GitHub and Docs links come from configuration. If a link is not configured, hide that button instead of linking nowhere.
- [ ] The licence name in the footer is read from the repository's LICENSE file; if there is none, leave it out.

### A9. Technical tasks

- [ ] Inspect the frontend: framework, router, styling system, existing theme support, component library. Use what is there.
- [ ] Add the public route `/` and redirect signed-in users to the dashboard.
- [ ] Add the colour tokens and theme switching (reuse the template's mechanism if it has one).
- [ ] Build each section as its own component under one `welcome` folder.
- [ ] Keep all page text in one content file, so a Kiswahili version can be added later without touching components.
- [ ] Code panel component: tabs, syntax colouring, copy button with confirmation.
- [ ] "How it works" diagram as inline SVG that uses the theme tokens.
- [ ] Responsive at 360, 768, 1024 and 1440 pixels wide; no horizontal scroll.
- [ ] Accessibility: semantic landmarks, one `h1`, heading order, keyboard access to menu, tabs and accordion, visible focus, text contrast at WCAG AA in both themes, `alt` text or `aria-hidden` on graphics.
- [ ] SEO and sharing: page title, meta description, Open Graph tags, favicon from the dove mark, `theme-color` for each theme.
- [ ] Performance: no layout shift from fonts or images, lazy-load anything below the fold that is heavy, no new large dependencies for animation.
- [ ] Tests: the page renders, the toggle switches and persists the theme, signed-in redirect works, tabs and accordion work by keyboard.

### A10. Done when

- [ ] All nine sections exist with the copy from this file.
- [ ] Light and dark mode both look finished at all four widths; screenshots of each are saved in `docs/welcome/`.
- [ ] No flash of the wrong theme on reload.
- [ ] Lint, type check, tests and production build all pass.
- [ ] Nothing on the page breaks the truthfulness rules in A8.

---

## Part B — Prompt for the coding agent

> You are building the public welcome page for **Njiwa-Mail**, an open-source, self-hosted email platform (send, receive and track transactional email through one API, on top of the owner's own mail server). The frontend template is already in this repository; the welcome page does not exist yet. Build it completely, following Part A of this file.
>
> **Start by reading the frontend.** Find the framework, router, styling system, component library and any existing theme mechanism. Use them. Do not add a second UI kit or a second styling system, and do not restructure existing pages.
>
> **Build exactly the nine sections in A2,** using the copy in A3, A4 and A5 word for word. You may tighten a sentence for layout, but do not add claims, features, numbers, testimonials, logos or pricing. Follow every rule in A8.
>
> **Design is the main job here.** Treat A6 as the brief: airmail and carrier pigeon, warm paper by day, night sky by dark. Before writing components, decide and write down in `docs/welcome/DESIGN.md`: the three fonts, the type scale, the spacing scale, the final colour tokens for both themes after contrast checks, and how the airmail stripe, postmark stamp and dove mark are used. Then build to that. Draw the dove logo, the stamp and the "How it works" diagram yourself as SVG. The result must not look like a default template: no purple gradients, no stock imagery, no emoji icons, no generic centred-hero-plus-three-cards layout repeated down the page. Vary the section layouts, keep generous spacing, and make the hero code panel the visual centre of the page.
>
> **Light and dark mode are both first-class.** Implement everything in A7. Design each theme on its own: check every section, the code panel, the diagram, hover and focus states in both. All colours come from tokens. Apply the theme before first paint so there is no flash.
>
> **Engineering.** Complete every task in A9. Keep all text in one content file. Make the page responsive at 360, 768, 1024 and 1440 pixels, keyboard accessible, and WCAG AA for contrast in both themes. Respect `prefers-reduced-motion`. Add no heavy dependencies.
>
> **Check your own work visually.** Run the app, open the page in both themes at all four widths, and take screenshots into `docs/welcome/`. Look at them critically: alignment, spacing rhythm, text wrapping, contrast, anything that looks unfinished. Fix what you find and re-take the screenshots. Do at least two rounds of this.
>
> **Finish only when every item in A10 is true.** No placeholder text, no "lorem ipsum", no dead links, no TODOs. End with a short report: files added and changed, commands to run and test, the fonts and tokens you chose, and anything you could not complete with the reason.
