// Kiswahili for the address checker, the SMTP relay page and the web view option.
// Keys are the exact English text passed to phrase(). Needs a native-speaker review.
export const swTools: Record<string, string> = {
  "A copy of this message is kept for 30 days behind a private link. Do not use it for one-time codes or reset links.":
    "Nakala ya ujumbe huu inahifadhiwa kwa siku 30 nyuma ya kiungo cha faragha. Usitumie kwa misimbo ya mara moja au viungo vya kubadilisha nenosiri.",
  Addresses: "Anwani",
  'Add a "view in browser" link': 'Ongeza kiungo cha "tazama kwenye kivinjari"',
  "Check addresses": "Kagua anwani",
  "Check at most 100 addresses at a time.": "Kagua anwani zisizozidi 100 kwa wakati mmoja.",
  Check: "Kagua",
  "Find out whether addresses look sendable before you mail them. This never sends mail and never contacts the recipient's mail server.":
    "Jua kama anwani zinaonekana kuweza kutumiwa kabla ya kuzitumia barua. Hii haitumi barua kamwe na haiwasiliani na seva ya barua ya mpokeaji.",
  "Last check-in": "Mara ya mwisho kuripoti",
  "Lets an application that cannot use the HTTP API send through SMTP. The password is an API key with the emails:send_raw scope.":
    "Inaruhusu programu isiyoweza kutumia HTTP API kutuma kupitia SMTP. Nenosiri ni API key yenye scope ya emails:send_raw.",
  Results: "Matokeo",
  "SMTP relay": "SMTP relay",
  "Separate with spaces, commas or new lines. Up to 100.":
    "Tenganisha kwa nafasi, koma au mistari mipya. Hadi 100.",
  "Settings for your application": "Mipangilio ya programu yako",
  "The relay has not checked in yet.": "Relay bado haijaripoti.",
  Why: "Kwa nini",
  "“Deliverable” means the domain can receive mail and nothing known blocks it. It is not a promise that the mailbox exists.":
    "“Deliverable” inamaanisha kikoa kinaweza kupokea barua na hakuna kinachojulikana kuzuia. Si ahadi kuwa sanduku la barua lipo.",
  Running: "Inafanya kazi",
  "Not running": "Haifanyi kazi",
  deliverable: "inaweza kufikishwa",
  risky: "ina hatari",
  undeliverable: "haiwezi kufikishwa",
  unknown: "haijulikani",
  suppressed: "imezuiwa",
  unsubscribed: "amejiondoa",
};
