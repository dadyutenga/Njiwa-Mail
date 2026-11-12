import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { Locale } from "@/types";

export interface Translations {
  nav: {
    overview: string;
    records: string;
    reports: string;
    profile: string;
    settings: string;
    example: string;
    home: string;
  };
  actions: {
    signIn: string;
    signOut: string;
    register: string;
    save: string;
    cancel: string;
    edit: string;
    delete: string;
    create: string;
    exportCsv: string;
    search: string;
    copy: string;
    print: string;
    previous: string;
    next: string;
  };
  dashboard: {
    tagline: string;
    title: string;
    subtitle: string;
    totalRecords: string;
    activity: string;
    completed: string;
    pending: string;
    quickActions: string;
    recentActivity: string;
  };
  auth: {
    welcomeBack: string;
    createAccount: string;
    forgotPassword: string;
    resetPassword: string;
    emailLabel: string;
    passwordLabel: string;
    demoNotice: string;
  };
  settings: {
    title: string;
    subtitle: string;
    account: string;
    appearance: string;
    notifications: string;
    language: string;
    theme: string;
  };
}

const dictionaries: Record<Locale, Translations> = {
  en: {
    nav: {
      overview: "Overview",
      records: "Records",
      reports: "Reports",
      profile: "Profile",
      settings: "Settings",
      example: "Example page",
      home: "Home",
    },
    actions: {
      signIn: "Sign in",
      signOut: "Sign out",
      register: "Register",
      save: "Save",
      cancel: "Cancel",
      edit: "Edit",
      delete: "Delete",
      create: "Create",
      exportCsv: "Export CSV",
      search: "Search",
      copy: "Copy",
      print: "Print",
      previous: "Previous",
      next: "Next",
    },
    dashboard: {
      tagline: "WORKSPACE OVERVIEW",
      title: "Welcome to your dashboard",
      subtitle: "A starting point for your next project. All values shown are mock data.",
      totalRecords: "Total records",
      activity: "Activity",
      completed: "Completed",
      pending: "Pending",
      quickActions: "Quick actions",
      recentActivity: "Recent activity",
    },
    auth: {
      welcomeBack: "Welcome back",
      createAccount: "Create an account",
      forgotPassword: "Forgot password",
      resetPassword: "Reset password",
      emailLabel: "Email",
      passwordLabel: "Password",
      demoNotice: "This is a local demo; no account or backend is connected.",
    },
    settings: {
      title: "Settings",
      subtitle: "Manage your preferences and workspace configuration.",
      account: "Account",
      appearance: "Appearance",
      notifications: "Notifications",
      language: "Language",
      theme: "Theme",
    },
  },
  sw: {
    nav: {
      overview: "Muhtasari",
      records: "Kumbukumbu",
      reports: "Ripoti",
      profile: "Wasifu",
      settings: "Mipangilio",
      example: "Ukurasa wa mfano",
      home: "Mwanzo",
    },
    actions: {
      signIn: "Ingia",
      signOut: "Toka",
      register: "Jiandikishe",
      save: "Hifadhi",
      cancel: "Ghairi",
      edit: "Hariri",
      delete: "Futa",
      create: "Unda",
      exportCsv: "Pakua CSV",
      search: "Tafuta",
      copy: "Nakili",
      print: "Chapa",
      previous: "Iliyotangulia",
      next: "Inayofuata",
    },
    dashboard: {
      tagline: "MUHTASARI WA ENEO-KAZI",
      title: "Karibu kwenye dashibodi yako",
      subtitle: "Mahali pa kuanzia mradi wako mpya. Nambari zote ni data ya mfano.",
      totalRecords: "Jumla ya rekodi",
      activity: "Shughuli",
      completed: "Zimekamilika",
      pending: "Zinasubiri",
      quickActions: "Hatua za haraka",
      recentActivity: "Shughuli za hivi karibuni",
    },
    auth: {
      welcomeBack: "Karibu tena",
      createAccount: "Fungua akaunti mpya",
      forgotPassword: "Umesahau nenosiri",
      resetPassword: "Weka nenosiri upya",
      emailLabel: "Barua pepe",
      passwordLabel: "Nenosiri",
      demoNotice: "Huu ni mfano wa ndani; hakuna seva iliyounganishwa.",
    },
    settings: {
      title: "Mipangilio",
      subtitle: "Simamia mapendeleo yako na usanidi wa eneo-kazi.",
      account: "Akaunti",
      appearance: "Muonekano",
      notifications: "Arifa",
      language: "Lugha",
      theme: "Mandhari",
    },
  },
};

interface I18nContextType {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: Translations;
}

const defaultI18nValue: I18nContextType = {
  locale: "en",
  setLocale: () => {},
  t: dictionaries.en,
};

const I18nContext = createContext<I18nContextType | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    try {
      const saved = localStorage.getItem("template-locale");
      return saved === "sw" ? "sw" : "en";
    } catch {
      return "en";
    }
  });

  const setLocale = (next: Locale) => {
    setLocaleState(next);
    try {
      localStorage.setItem("template-locale", next);
    } catch {
      /* ignore */
    }
  };

  useEffect(() => {
    if (typeof document !== "undefined") {
      document.documentElement.lang = locale;
    }
  }, [locale]);

  return (
    <I18nContext.Provider value={{ locale, setLocale, t: dictionaries[locale] }}>
      {children}
    </I18nContext.Provider>
  );
}

export function useI18n() {
  const context = useContext(I18nContext);
  return context ?? defaultI18nValue;
}

const swPhrases: Record<string, string> = {
  "Nothing to reconcile": "Hakuna cha kuthibitisha",
  "No message is waiting for an outcome.": "Hakuna ujumbe unaosubiri matokeo.",
  "What the evidence suggests": "Ushahidi unapendekeza nini",
  "Looks accepted": "Inaonekana imekubaliwa",
  "Looks failed": "Inaonekana imeshindwa",
  "No suggestion": "Hakuna pendekezo",
  "Accept suggestion": "Kubali pendekezo",
  "Waiting since": "Inasubiri tangu",
  "Outcome recorded.": "Matokeo yamerekodiwa.",
  "Export CSV": "Pakua CSV",
  "Export record": "Pakua rekodi",
  "Delete record": "Futa rekodi",
  "Delete this message record?": "Kufuta rekodi ya ujumbe huu?",
  "Message record deleted.": "Rekodi ya ujumbe imefutwa.",
  "Two-step sign-in": "Kuingia kwa hatua mbili",
  "Set up two-step sign-in": "Weka kuingia kwa hatua mbili",
  "Two-step sign-in is on for your account.":
    "Kuingia kwa hatua mbili kumewashwa kwa akaunti yako.",
  "Authentication code": "Msimbo wa uthibitisho",
  "Code from the app": "Msimbo kutoka kwenye programu",
  "Turn on": "Washa",
  "Turn off": "Zima",
  "I saved them": "Nimevihifadhi",
  "Save these recovery codes now. Each works once and they are not shown again.":
    "Hifadhi misimbo hii ya kurejesha sasa. Kila mmoja hufanya kazi mara moja na hautaonyeshwa tena.",
  "Enter the code from your authenticator app, or a recovery code.":
    "Weka msimbo kutoka kwenye programu yako ya uthibitisho, au msimbo wa kurejesha.",
  "Installation operator (can change delivery infrastructure, users and settings)":
    "Mwendeshaji wa usakinishaji (anaweza kubadilisha miundombinu ya utumaji, watumiaji na mipangilio)",
  "Require two-step sign-in for every admin":
    "Lazimisha kuingia kwa hatua mbili kwa kila msimamizi",
  Operator: "Mwendeshaji",
  "Two-step": "Hatua mbili",
  "Make operator": "Mfanye mwendeshaji",
  "Remove operator": "Ondoa uendeshaji",
  "Reset two-step": "Weka upya hatua mbili",
  "Sending mode": "Hali ya kutuma",
  "Live — send to real recipients": "Live — tuma kwa wapokeaji halisi",
  "Sandbox — capture only": "Sandbox — hifadhi kwa majaribio tu",
  "Switch project": "Badilisha mradi",
  "Project workspace": "Eneo la kazi la mradi",
  "A project opens in Live. Choose Sandbox only when sending a test. You will land on its setup checklist.":
    "Mradi hufunguka katika Live. Chagua Sandbox unapotuma jaribio tu. Utafungua orodha ya maandalizi.",
  "Shared access": "Ufikiaji wa pamoja",
  "Every enabled user is an admin and can access all pages and settings.":
    "Kila mtumiaji aliyewezeshwa ni msimamizi na anaweza kufikia kurasa na mipangilio yote.",
  "Administration settings": "Mipangilio ya usimamizi",
  Overview: "Muhtasari",
  Messages: "Ujumbe",
  "Message detail": "Maelezo ya ujumbe",
  Projects: "Miradi",
  "Project settings": "Mipangilio ya mradi",
  "API keys": "Funguo za API",
  Senders: "Watumaji",
  Templates: "Violezo",
  "Template detail": "Maelezo ya kiolezo",
  Inbound: "Barua zinazoingia",
  "Inbound message": "Ujumbe ulioingia",
  "Inbound addresses": "Anwani za barua zinazoingia",
  "Inbound mail": "Barua zinazoingia",
  Webhooks: "Webhooks",
  "Webhook deliveries": "Uwasilishaji wa webhook",
  Suppressions: "Vizuiwa",
  Bounces: "Barua zilizorejeshwa",
  Alerts: "Arifa",
  "Alert channels": "Njia za arifa",
  Domains: "Vikoa",
  "DNS checks": "Ukaguzi wa DNS",
  Sandbox: "Sandbox",
  "Sandbox captures": "Nakala za sandbox",
  "Sandbox capture": "Nakala ya sandbox",
  Analytics: "Takwimu",
  "Audit log": "Kumbukumbu ya ukaguzi",
  Settings: "Mipangilio",
  Profile: "Wasifu",
  "Recipient privacy": "Faragha ya mpokeaji",
  "Team users": "Watumiaji wa timu",
  Page: "Ukurasa",
  Home: "Mwanzo",
  "Open navigation": "Fungua urambazaji",
  "Choose a page to navigate to.": "Chagua ukurasa wa kwenda.",
  "Toggle sidebar": "Kunja au fungua upau",
  "Njiwa-Mail dashboard": "Dashibodi ya Njiwa-Mail",
  "Njiwa-Mail · Built in Tanzania": "Njiwa-Mail · Imejengwa Tanzania",
  "Search pages": "Tafuta kurasa",
  "Find a page by name or press Control K to open search.":
    "Tafuta ukurasa kwa jina au bonyeza Control K kufungua utafutaji.",
  "Search pages…": "Tafuta kurasa…",
  Notifications: "Arifa",
  "Loading…": "Inapakia…",
  "open alerts": "arifa zilizo wazi",
  "Open user menu": "Fungua menyu ya mtumiaji",
  "Sign out": "Toka",
  "Welcome back": "Karibu tena",
  "Forgot password": "Umesahau nenosiri",
  "Reset password": "Weka nenosiri upya",
  "Use your Njiwa-Mail operator account.": "Tumia akaunti yako ya mwendeshaji wa Njiwa-Mail.",
  Email: "Barua pepe",
  Password: "Nenosiri",
  "Send Instructions": "Tuma maelekezo",
  "Sign in": "Ingia",
  "Forgot password?": "Umesahau nenosiri?",
  "If this account exists, reset instructions have been sent.":
    "Akaunti hii ikipo, maelekezo ya kuweka upya yametumwa.",
  "Reset token is missing.": "Tokeni ya kuweka upya haipo.",
  "Password updated. You can sign in now.": "Nenosiri limebadilishwa. Unaweza kuingia sasa.",
  "Request failed": "Ombi limeshindikana",
  "Password must be at least 8 characters": "Nenosiri liwe na herufi 8 au zaidi",
  "Password must be at least 12 characters": "Nenosiri liwe na herufi 12 au zaidi",
  "Loading overview…": "Inapakia muhtasari…",
  "Submitted today": "Zilizowasilishwa leo",
  "Queue depth": "Kina cha foleni",
  "Webhook backlog": "Webhook zinazosubiri",
  "Open alerts": "Arifa zilizo wazi",
  "Current operational data from the mail service.":
    "Data ya sasa ya uendeshaji kutoka huduma ya barua pepe.",
  "Message states": "Hali za ujumbe",
  "View messages →": "Tazama ujumbe →",
  "Service health": "Afya ya huduma",
  Configured: "Imesanidiwa",
  "Not configured": "Haijasanidiwa",
  Healthy: "Nzima",
  Unhealthy: "Si nzima",
  "Live data from the Njiwa-Mail admin API.": "Data hai kutoka API ya usimamizi ya Njiwa-Mail.",
  Close: "Funga",
  "Add new": "Ongeza mpya",
  "Run DNS check": "Fanya ukaguzi wa DNS",
  "DNS check failed": "Ukaguzi wa DNS umeshindikana",
  "Add this TXT challenge to DNS, then verify the domain.":
    "Ongeza changamoto hii ya TXT kwenye DNS, kisha thibitisha kikoa.",
  "Copy this secret now. It will not be shown again.": "Nakili siri hii sasa. Haitaonyeshwa tena.",
  Dismiss: "Funga",
  Create: "Unda",
  Actions: "Vitendo",
  "No records yet.": "Hakuna kumbukumbu bado.",
  Previous: "Iliyotangulia",
  Next: "Inayofuata",
  Yes: "Ndiyo",
  No: "Hapana",
  "People who can sign in. Every user can manage the workspace.":
    "Watu wanaoweza kuingia. Kila mtumiaji anaweza kusimamia eneo la kazi.",
  "Enter four nonnegative whole numbers.": "Weka nambari nne nzima zisizo hasi.",
  "Reason for removing this suppression": "Sababu ya kuondoa kizuizi hiki",
  Project: "Mradi",
  Environment: "Mazingira",
  "Project name": "Jina la mradi",
  Label: "Lebo",
  "Scopes (comma-separated)": "Idhini (tenganisha kwa koma)",
  "Lifetime in hours": "Muda wa maisha kwa saa",
  "Sender email": "Barua pepe ya mtumaji",
  "Display name": "Jina la kuonyesha",
  "Template slug": "Slug ya kiolezo",
  Description: "Maelezo",
  "Local part": "Sehemu ya ndani",
  Route: "Njia",
  "Webhook endpoint ID": "Kitambulisho cha webhook",
  "Forward to": "Sambaza kwa",
  "HTTPS URL": "URL ya HTTPS",
  Reason: "Sababu",
  Kind: "Aina",
  Target: "Lengo",
  Domain: "Kikoa",
  Purpose: "Madhumuni",
  "Temporary password (12+ characters)": "Nenosiri la muda (herufi 12+)",
  Role: "Jukumu",
  disable: "zima",
  enable: "washa",
  "edit quotas": "hariri viwango",
  remove: "ondoa",
  "rotate secret": "badilisha siri",
  rotate: "badilisha",
  revoke: "batilisha",
  replay: "rudia",
  verify: "thibitisha",
  acknowledge: "kubali",
  "created at": "imeundwa",
  "send per minute": "utumaji kwa dakika",
  "send per day": "utumaji kwa siku",
  prefix: "kiambishi",
  project: "mradi",
  mode: "mazingira",
  label: "lebo",
  scopes: "idhini",
  status: "hali",
  email: "barua pepe",
  "display name": "jina la kuonyesha",
  slug: "slug",
  description: "maelezo",
  "published version": "toleo lililochapishwa",
  "draft version": "toleo la rasimu",
  "received at": "imepokelewa",
  "header from": "kutoka kichwani",
  "envelope to": "kwa bahasha",
  subject: "mada",
  "local part": "sehemu ya ndani",
  enabled: "imewashwa",
  route: "njia",
  url: "url",
  events: "matukio",
  "event type": "aina ya tukio",
  state: "hali",
  attempts: "majaribio",
  "last response code": "msimbo wa mwisho",
  recipient: "mpokeaji",
  reason: "sababu",
  source: "chanzo",
  type: "aina",
  "status code": "msimbo wa hali",
  kind: "aina",
  summary: "muhtasari",
  "acknowledged at": "imekubaliwa",
  target: "lengo",
  name: "jina",
  purpose: "madhumuni",
  "verified at": "imethibitishwa",
  "spf ok": "spf sawa",
  "dkim ok": "dkim sawa",
  "dmarc ok": "dmarc sawa",
  domain: "kikoa",
  "record type": "aina ya rekodi",
  pass: "imepita",
  expected: "inayotarajiwa",
  observed: "iliyoonekana",
  id: "kitambulisho",
  template: "kiolezo",
  "expires at": "inaisha",
  actor: "mhusika",
  action: "kitendo",
  "target type": "aina ya lengo",
  "target id": "kitambulisho cha lengo",
  at: "wakati",
  disabled: "imezimwa",
  "last login at": "mwisho kuingia",
  "Loading messages…": "Inapakia ujumbe…",
  Created: "Imeundwa",
  Template: "Kiolezo",
  State: "Hali",
  Attempts: "Majaribio",
  "No messages match this filter.": "Hakuna ujumbe unaolingana na kichujio hiki.",
  "Loading message…": "Inapakia ujumbe…",
  Raw: "Ghafi",
  "Bounce state": "Hali ya kurejeshwa",
  None: "Hakuna",
  "Resolve uncertain outcome": "Tatua matokeo yasiyo hakika",
  Outcome: "Matokeo",
  Submitted: "Imekubaliwa",
  Failed: "Imeshindikana",
  "Evidence note": "Maelezo ya ushahidi",
  "Mail server log reference": "Rejea ya kumbukumbu ya seva ya barua",
  Resolve: "Tatua",
  "Resolution failed": "Utatuzi umeshindikana",
  "Event timeline": "Ratiba ya matukio",
  "Loading project…": "Inapakia mradi…",
  "Manage sandbox and live sending rules.": "Simamia sheria za utumaji wa sandbox na live.",
  "Save failed": "Kuhifadhi kumeshindikana",
  "Project not found": "Mradi haujapatikana",
  Save: "Hifadhi",
  "Loading template…": "Inapakia kiolezo…",
  "Shared template": "Kiolezo cha pamoja",
  "Draft editor": "Kihariri cha rasimu",
  "Draft saved": "Rasimu imehifadhiwa",
  Versions: "Matoleo",
  Published: "Imechapishwa",
  Draft: "Rasimu",
  "Preview ready": "Onyesho liko tayari",
  "Publish this version?": "Chapisha toleo hili?",
  "Version published": "Toleo limechapishwa",
  "Sandbox test send": "Utumaji wa majaribio wa sandbox",
  "Sandbox message queued": "Ujumbe wa sandbox umewekwa foleni",
  "Revoke this session?": "Batilisha kipindi hiki?",
  "Email:": "Barua pepe:",
  "Role:": "Jukumu:",
  Sessions: "Vipindi",
  Browser: "Kivinjari",
  Revoke: "Batilisha",
  "Export downloaded.": "Data imepakuliwa.",
  "Export failed": "Upakuaji umeshindikana",
  "Enter the same email to confirm deletion.": "Andika barua pepe ileile kuthibitisha ufutaji.",
  "Recipient data deleted.": "Data ya mpokeaji imefutwa.",
  "Deletion failed": "Ufutaji umeshindikana",
  "Export data": "Pakua data",
  "Delete recipient data": "Futa data ya mpokeaji",
  "Loading settings…": "Inapakia mipangilio…",
  "Operator limits and retention settings.": "Viwango vya mwendeshaji na mipangilio ya uhifadhi.",
  "Recorded mail activity from the backend.": "Shughuli za barua zilizorekodiwa kutoka seva.",
  "Loading analytics…": "Inapakia takwimu…",
  "No analytics recorded yet.": "Hakuna takwimu bado.",
  "From:": "Kutoka:",
  "To:": "Kwa:",
  "Text content": "Maudhui ya maandishi",
  "No text content": "Hakuna maandishi",
  Attachments: "Viambatisho",
  "You do not have permission to access this resource or view this page.":
    "Huna ruhusa kufikia rasilimali hii au kuona ukurasa huu.",
  "The page you are looking for does not exist or may have been moved.":
    "Ukurasa unaoutafuta haupo au umehamishwa.",
  "An unexpected server error occurred. Please try again later.":
    "Hitilafu isiyotarajiwa imetokea. Jaribu tena baadaye.",
  "An unexpected status occurred.": "Hali isiyotarajiwa imetokea.",
  "Return to Dashboard": "Rudi kwenye dashibodi",
  "Access Denied": "Ufikiaji umekataliwa",
  "Page Not Found": "Ukurasa haujapatikana",
  "Server Error": "Hitilafu ya seva",
  "Page not found": "Ukurasa haujapatikana",
  "Return home": "Rudi mwanzo",
  "Something went wrong": "Kuna tatizo",
  "Njiwa-Mail home": "Mwanzo wa Njiwa-Mail",
  "Checking session…": "Inakagua kipindi…",
  "Metadata only. Message content and recipients are not shown here.":
    "Data ya meta pekee. Maudhui ya ujumbe na wapokeaji hawaonyeshwi hapa.",
  "← Messages": "← Ujumbe",
  "← Projects": "← Miradi",
  "Message ": "Ujumbe ",
  "Check the mail server logs before resolving. This records the observed outcome; it does not send again.":
    "Kagua kumbukumbu za seva ya barua kabla ya kutatua. Hii inarekodi matokeo yaliyoonekana; haitumi tena.",
  "Record resolution": "Rekodi utatuzi",
  Enabled: "Imewashwa",
  "Restrict recipients": "Zuia wapokeaji",
  "Approved origins, one per line": "Asili zilizoidhinishwa, moja kwa kila mstari",
  "Recipient allowlist, one email or @domain per line":
    "Orodha ya wapokeaji, barua pepe moja au @kikoa kwa kila mstari",
  "Saved.": "Imehifadhiwa.",
  "Save environment": "Hifadhi mazingira",
  "Settings saved.": "Mipangilio imehifadhiwa.",
  "Save settings": "Hifadhi mipangilio",
  "Export or remove one recipient’s stored data in a specific project and environment.":
    "Pakua au futa data iliyohifadhiwa ya mpokeaji mmoja katika mradi na mazingira mahususi.",
  "Recipient email": "Barua pepe ya mpokeaji",
  "Re-enter email to confirm deletion": "Andika tena barua pepe kuthibitisha ufutaji",
  sandbox: "sandbox",
  live: "live",
  Denied: "Imekataliwa",
  Private: "Faragha",
  "Operator dashboard": "Dashibodi ya mwendeshaji",
  Mail: "Barua",
  Workspace: "Eneo la kazi",
  Developers: "Wasanidi",
  Admin: "Usimamizi",
  Dashboard: "Dashibodi",
  "Setup checklist": "Orodha ya maandalizi",
  Inbox: "Kikasha",
  Sent: "Zilizotumwa",
  Scheduled: "Zilizopangwa",
  "Bulk send": "Tuma kwa wengi",
  "Temporary inboxes": "Vikasha vya muda",
  "DNS setup guide": "Mwongozo wa DNS",
  Mailboxes: "Masanduku ya barua",
  "Sender addresses": "Anwani za watumaji",
  Automations: "Kanuni za kiotomatiki",
  Complaints: "Malalamiko",
  "Message log": "Kumbukumbu za ujumbe",
  "Unknown queue": "Foleni isiyojulikana",
  "API docs": "Nyaraka za API",
  Users: "Watumiaji",
  "Mail server": "Seva ya barua",
  System: "Mfumo",
  Compose: "Andika",
  Search: "Tafuta",
  Live: "Halisi",
  "New project": "Mradi mpya",
  "Switch project or environment": "Badilisha mradi au mazingira",
  "Search projects…": "Tafuta miradi…",
  "No project matches.": "Hakuna mradi unaolingana.",
  "Skip to content": "Ruka hadi maudhui",
  "Back to my projects": "Rudi kwenye miradi yangu",
  "Mail sent here goes to real recipients.": "Barua zinazotumwa hapa zinawafikia wapokeaji halisi.",
  "Mail is captured, nothing leaves the server.":
    "Barua zinahifadhiwa, hakuna kinachotoka kwenye seva.",
  "Create project": "Unda mradi",
  "Choose a project": "Chagua mradi",
  "Create your first project": "Unda mradi wako wa kwanza",
  "Set up Njiwa-Mail": "Andaa Njiwa-Mail",
  Welcome: "Karibu",
  Retry: "Jaribu tena",
  Cancel: "Ghairi",
  Confirm: "Thibitisha",
  Delete: "Futa",
  Open: "Fungua",
  Back: "Rudi",
  Reply: "Jibu",
  Forward: "Sambaza",
  Archive: "Hifadhi kumbukumbu",
  Unarchive: "Rudisha kikashani",
  "Mark unread": "Weka kama haijasomwa",
  "Block sender": "Zuia mtumaji",
  Send: "Tuma",
  "Send now": "Tuma sasa",
  Schedule: "Panga",
  "Send to sandbox": "Tuma kwenye sandbox",
  "Save draft": "Hifadhi rasimu",
  Drafts: "Rasimu",
  Write: "Andika",
  Publish: "Chapisha",
  Duplicate: "Nakili",
  Restore: "Rejesha",
  "Check now": "Kagua sasa",
  "Add domain": "Ongeza kikoa",
  "Add a sender": "Ongeza mtumaji",
  "Create mailbox": "Unda sanduku la barua",
  "Create API key": "Unda ufunguo wa API",
  "New rule": "Kanuni mpya",
  "New template": "Kiolezo kipya",
  "New announcement": "Tangazo jipya",
  Start: "Anza",
  Pause: "Sitisha",
  Resume: "Endelea",
  "Test connection": "Jaribu muunganisho",
  "Send test email": "Tuma barua ya majaribio",
  "Copy it now. It will not be shown again.": "Inakili sasa. Haitaonyeshwa tena.",
  "I have stored it": "Nimeihifadhi",
  From: "Kutoka",
  To: "Kwa",
  Subject: "Mada",
  Message: "Ujumbe",
  Recipient: "Mpokeaji",
  Sender: "Mtumaji",
  Source: "Chanzo",
  Address: "Anwani",
  Status: "Hali",
  "Last used": "Ilitumika mwisho",
  Unread: "Haijasomwa",
  All: "Zote",
  Mailbox: "Sanduku la barua",
  "All mailboxes": "Masanduku yote",
  "No messages": "Hakuna ujumbe",
  "No domains yet": "Bado hakuna vikoa",
  "No mailboxes": "Hakuna masanduku ya barua",
  "No sender addresses": "Hakuna anwani za watumaji",
  "No API keys": "Hakuna funguo za API",
  "No templates": "Hakuna violezo",
  "No rules yet": "Bado hakuna kanuni",
  "No announcements yet": "Bado hakuna matangazo",
  "Nothing is scheduled": "Hakuna kilichopangwa",
  "Choose a message to read it.": "Chagua ujumbe ili kuusoma.",
  "Create a mailbox": "Unda sanduku la barua",
  "Connect a domain first": "Unganisha kikoa kwanza",
  "Verify a domain first": "Thibitisha kikoa kwanza",
  "Connect mail server": "Unganisha seva ya barua",
  "Create a project": "Unda mradi",
  "Add a domain": "Ongeza kikoa",
  "Publish DNS records": "Chapisha rekodi za DNS",
  "Add a sender address": "Ongeza anwani ya mtumaji",
  "Create an API key": "Unda ufunguo wa API",
  "Send a test email": "Tuma barua ya majaribio",
  optional: "si lazima",
  "Delivery infrastructure": "Miundombinu ya utumaji barua",
  "Getting started": "Kuanza",
  "How will you send email?": "Utatumaje barua pepe?",
  "Send from my VPS": "Tuma kutoka kwenye VPS yangu",
  "Connect an existing mail server": "Unganisha seva ya barua iliyopo",
  Continue: "Endelea",
  "Continue existing installation": "Endelea na usakinishaji uliopo",
  Ready: "Tayari",
  "Not started": "Haijaanza",
  Checking: "Inakagua",
  "Needs action": "Inahitaji hatua",
  "Action needed outside Njiwa-Mail": "Hatua inahitajika nje ya Njiwa-Mail",
  Error: "Hitilafu",
  "Out of date": "Imepitwa na wakati",
  "Optional, skipped": "Si lazima, imerukwa",
  "Run checks again": "Kagua tena",
  "Last checked": "Ilikaguliwa mara ya mwisho",
  "required steps": "hatua za lazima",
  "Active mode": "Njia inayotumika",
  "settings revision": "toleo la mipangilio",
  "Mail server type": "Aina ya seva ya barua",
  "Generic SMTP": "SMTP ya kawaida",
  "Your mail server": "Seva yako ya barua",
  "Open Delivery infrastructure": "Fungua miundombinu ya utumaji barua",
  "Delivery mode saved.": "Njia ya utumaji imehifadhiwa.",
  "Installation setup": "Maandalizi ya usakinishaji",
  "Project setup": "Maandalizi ya mradi",
};

export function usePhrase() {
  const { locale } = useI18n();
  return (text: string) => (locale === "sw" ? (swPhrases[text] ?? text) : text);
}
