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
