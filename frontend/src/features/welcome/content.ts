export const content = {
  brand: "Njiwa-Mail",
  nav: {
    features: "Features",
    how: "How it works",
    docs: "Docs",
    github: "GitHub",
    signIn: "Sign in",
    menu: "Open menu",
    closeMenu: "Close menu",
  },
  hero: {
    eyebrow: "SELF-HOSTED TRANSACTIONAL EMAIL",
    title: "Your own mail carrier.",
    description:
      "Njiwa-Mail is an open-source email platform for your apps. Send, receive and track email through one API, on servers you control.",
    github: "View on GitHub",
    copy: "Copy request",
    copied: "Copied",
    response: "Response",
    stamp: "FROM YOUR SERVER · TO THE WORLD",
  },
  how: {
    eyebrow: "01 / THE ROUTE",
    title: "A clear path for every message.",
    description:
      "Your app hands a message to Njiwa-Mail. Your mail server sends it onward. Replies and bounces find their way back.",
    nodes: ["Your app", "Njiwa-Mail", "Your mail server", "Recipient"],
    return: "Replies & bounces return",
  },
  features: {
    eyebrow: "02 / WHAT IT DOES",
    title: "Built for the messages that matter.",
    items: [
      {
        title: "Send over HTTP or SMTP",
        text: "One API call or your framework's normal mail settings. Both go through the same queue and rules.",
      },
      {
        title: "Never sends twice",
        text: "Every request carries an idempotency key, so a retry from your app cannot create a second email.",
      },
      {
        title: "Safe retries",
        text: "Njiwa-Mail retries only when it is certain the mail server did not accept the message. When it cannot tell, it stops and tells you.",
      },
      {
        title: "Receive email too",
        text: "Replies and inbound mail arrive parsed as JSON, with attachments, through the API or a webhook.",
      },
      {
        title: "Bounces and complaints",
        text: "Bad addresses and spam reports are picked up and suppressed automatically, so your sender reputation stays clean.",
      },
      {
        title: "Signed webhooks",
        text: "Your app is told what happened to each message, with signed requests, retries and replay.",
      },
      {
        title: "Templates in English and Kiswahili",
        text: "Versioned templates with a live preview. Published versions never change under a queued message.",
      },
      {
        title: "Fair queue",
        text: "Each app takes its turn, so one app's burst cannot delay another app's login codes.",
      },
    ],
  },
  status: {
    eyebrow: "03 / HONEST STATUS",
    title: "Know exactly what happened.",
    description: "Njiwa-Mail never claims “delivered”. It shows what it can prove.",
    states: [
      { title: "Queued", text: "The message is safely stored." },
      { title: "Submitting", text: "Your mail server is being contacted." },
      { title: "Submitted", text: "Your mail server accepted the message." },
      { title: "Bounce or complaint", text: "A later signal is recorded if one arrives." },
    ],
  },
  developer: {
    eyebrow: "04 / FOR DEVELOPERS",
    title: "One request. A durable queue.",
    description:
      "Use your backend to send a transactional message. Give each logical event its own idempotency key and reuse that key for retries.",
    guide: "Read the integration guide",
    tabs: ["cURL", "Go", "Laravel", "Django", "Node"],
  },
  selfHost: {
    eyebrow: "05 / YOUR INFRASTRUCTURE",
    title: "Your server. Your rules.",
    description:
      "Njiwa-Mail runs beside your own mail server with Docker Compose. Your apps keep one API while your mail server handles submission.",
    quick: "Quick start",
    github: "Explore the source",
  },
  faq: {
    eyebrow: "06 / QUESTIONS",
    title: "Good to know.",
    items: [
      {
        question: "What is Njiwa-Mail?",
        answer:
          "An open-source email platform you host yourself. Your apps send and receive email through one API, using your own mail server.",
      },
      {
        question: "Do I need to pay per email?",
        answer: "No. You run it on your own server, so there is no per-message fee.",
      },
      {
        question: "Does it replace my mail server?",
        answer:
          "No. It sits beside a mail server such as Mailcow, which still does the actual delivery and DKIM signing.",
      },
      {
        question: "Can I send newsletters?",
        answer:
          "No. Njiwa-Mail is for transactional email: login codes, password resets, receipts and notices.",
      },
      {
        question: "How do I know an email arrived?",
        answer:
          "Njiwa-Mail shows what it can prove: that your mail server accepted the message, and whether a bounce or complaint came back. It does not claim inbox placement or that someone read it.",
      },
      {
        question: "Can I test without sending real email?",
        answer: "Yes. Sandbox keys capture messages locally and never contact a real mail server.",
      },
    ],
  },
  footer: {
    description: "Transactional email on servers you control.",
    origin: "Built in Tanzania",
  },
  theme: { light: "Light theme", dark: "Dark theme" },
  samples: {
    cURL: `curl -X POST https://mail.example.com/v1/emails \\
  -H "Authorization: Bearer $NJIWA_API_KEY" \\
  -H "Idempotency-Key: otp-event-001" \\
  -H "Content-Type: application/json" \\
  -d '{"from":"noreply@notify.example.com","to":"user@example.org","template":"otp","locale":"sw","variables":{"code":"482913","expires_in_minutes":5}}'`,
    Go: `req, _ := http.NewRequest("POST", "https://mail.example.com/v1/emails", bytes.NewBuffer(payload))
req.Header.Set("Authorization", "Bearer " + os.Getenv("NJIWA_API_KEY"))
req.Header.Set("Idempotency-Key", "otp-event-001")
req.Header.Set("Content-Type", "application/json")
resp, err := http.DefaultClient.Do(req)`,
    Laravel: `$response = Http::withToken(env('NJIWA_API_KEY'))
    ->withHeaders(['Idempotency-Key' => 'otp-event-001'])
    ->post('https://mail.example.com/v1/emails', $payload);`,
    Django: `response = requests.post(
    'https://mail.example.com/v1/emails',
    headers={'Authorization': 'Bearer ' + os.environ['NJIWA_API_KEY'],
             'Idempotency-Key': 'otp-event-001'},
    json=payload,
)`,
    Node: `const response = await fetch('https://mail.example.com/v1/emails', {
  method: 'POST',
  headers: { Authorization: 'Bearer ' + process.env.NJIWA_API_KEY,
             'Idempotency-Key': 'otp-event-001',
             'Content-Type': 'application/json' },
  body: JSON.stringify(payload),
});`,
  },
  compose: `cp .env.example .env
# Set database and encryption values in .env
docker compose up -d --build`,
} as const;
