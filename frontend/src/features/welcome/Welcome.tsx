import { useEffect, useState, type KeyboardEvent } from "react";
import { Link, Navigate } from "react-router-dom";
import { ArrowRight, Check, ChevronDown, Copy, Menu, Moon, Sun, X } from "lucide-react";
import { useAuth } from "@/features/auth/auth";
import { content as c } from "./content";
import { welcomeLinks } from "./config";
import "./welcome.css";

function Dove({ size = 38 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <path
        d="M6 31c8-1 13-5 17-13 3 4 6 6 12 6 3 0 5-1 8-3-2 7-7 12-15 13l-6 8-2-9C14 35 9 34 6 31Z"
        fill="currentColor"
      />
      <path
        d="M16 27c5-8 9-14 18-20-2 8-4 15-8 20M35 20l8-2-6 5"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
function ThemeSwitch() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));
  useEffect(() => {
    const update = () => setDark(document.documentElement.classList.contains("dark"));
    window.addEventListener("themechange", update);
    return () => window.removeEventListener("themechange", update);
  }, []);
  return (
    <button
      className="welcome-icon-button"
      aria-label={dark ? c.theme.dark : c.theme.light}
      aria-pressed={dark}
      onClick={() => {
        const next = !dark;
        localStorage.setItem("theme", next ? "dark" : "light");
        document.documentElement.classList.toggle("dark", next);
        setDark(next);
        window.dispatchEvent(new Event("themechange"));
      }}
    >
      {dark ? <Moon size={19} /> : <Sun size={19} />}
    </button>
  );
}
function Navigation() {
  const [open, setOpen] = useState(false);
  const links = [
    { href: "#features", label: c.nav.features },
    { href: "#how", label: c.nav.how },
    ...(welcomeLinks.docs ? [{ href: welcomeLinks.docs, label: c.nav.docs }] : []),
    ...(welcomeLinks.github ? [{ href: welcomeLinks.github, label: c.nav.github }] : []),
  ];
  return (
    <header className="welcome-nav">
      <div className="welcome-container nav-inner">
        <a className="welcome-brand" href="/" aria-label={c.brand}>
          <Dove />
          <span>{c.brand}</span>
        </a>
        <nav className="nav-links" aria-label="Primary">
          {links.map((link) => (
            <a key={link.label} href={link.href}>
              {link.label}
            </a>
          ))}
        </nav>
        <div className="nav-actions">
          <ThemeSwitch />
          <Link className="welcome-button nav-signin" to="/login">
            {c.nav.signIn}
            <ArrowRight size={16} />
          </Link>
          <button
            className="welcome-icon-button mobile-menu-button"
            aria-label={open ? c.nav.closeMenu : c.nav.menu}
            aria-expanded={open}
            aria-controls="welcome-mobile-nav"
            onClick={() => setOpen(!open)}
          >
            {open ? <X size={20} /> : <Menu size={20} />}
          </button>
        </div>
      </div>
      {open && (
        <nav id="welcome-mobile-nav" className="mobile-nav" aria-label="Mobile">
          {links.map((link) => (
            <a key={link.label} href={link.href} onClick={() => setOpen(false)}>
              {link.label}
            </a>
          ))}
          <Link to="/login" onClick={() => setOpen(false)}>
            {c.nav.signIn}
          </Link>
        </nav>
      )}
    </header>
  );
}
function highlighted(code: string) {
  return code
    .split(/("[^"\n]*"|'[^'\n]*'|#[^\n]*|\b(?:POST|Bearer|const|await|response)\b)/g)
    .map((part, index) => {
      const kind =
        part.startsWith('"') || part.startsWith("'")
          ? "string"
          : part.startsWith("#")
            ? "comment"
            : /^(POST|Bearer|const|await|response)$/.test(part)
              ? "keyword"
              : "plain";
      return (
        <span className={`syntax-${kind}`} key={index}>
          {part}
        </span>
      );
    });
}
function CodePanel({ hero = false }: { hero?: boolean }) {
  const tabs = c.developer.tabs;
  const [active, setActive] = useState<(typeof tabs)[number]>("cURL");
  const [copied, setCopied] = useState(false);
  const code = c.samples[active];
  const copy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  };
  const onKey = (e: KeyboardEvent<HTMLButtonElement>, index: number) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft" && e.key !== "Home" && e.key !== "End")
      return;
    e.preventDefault();
    const next =
      e.key === "Home"
        ? 0
        : e.key === "End"
          ? tabs.length - 1
          : (index + (e.key === "ArrowRight" ? 1 : -1) + tabs.length) % tabs.length;
    setActive(tabs[next]);
    document.getElementById(`welcome-tab-${hero ? "hero" : "dev"}-${next}`)?.focus();
  };
  return (
    <div className={`welcome-code ${hero ? "hero-code" : ""}`}>
      <div className="code-top">
        <div className="code-dots" aria-hidden="true">
          <i />
          <i />
          <i />
        </div>
        <span>POST /v1/emails</span>
        <button
          onClick={copy}
          aria-label={copied ? c.hero.copied : c.hero.copy}
          className="copy-button"
        >
          {copied ? <Check size={15} /> : <Copy size={15} />}
          <span>{copied ? c.hero.copied : c.hero.copy}</span>
        </button>
      </div>
      {!hero && (
        <div className="code-tabs" role="tablist" aria-label="Code language">
          {tabs.map((tab, i) => (
            <button
              key={tab}
              id={`welcome-tab-dev-${i}`}
              role="tab"
              aria-selected={active === tab}
              tabIndex={active === tab ? 0 : -1}
              onClick={() => setActive(tab)}
              onKeyDown={(e) => onKey(e, i)}
            >
              {tab}
            </button>
          ))}
        </div>
      )}
      <div
        role={hero ? undefined : "tabpanel"}
        aria-labelledby={hero ? undefined : `welcome-tab-dev-${tabs.indexOf(active)}`}
        className="code-body"
      >
        <pre>
          <code>{highlighted(code)}</code>
        </pre>
      </div>
      {hero && (
        <div className="code-response">
          <span>{c.hero.response}</span>
          <strong>202</strong>
          <code>{'{ "status": "queued" }'}</code>
        </div>
      )}
    </div>
  );
}
function Hero() {
  return (
    <section className="welcome-hero welcome-container">
      <div className="hero-copy">
        <p className="eyebrow">{c.hero.eyebrow}</p>
        <h1>{c.hero.title}</h1>
        <p className="hero-description">{c.hero.description}</p>
        <div className="hero-actions">
          <Link className="welcome-button" to="/login">
            {c.nav.signIn}
            <ArrowRight size={18} />
          </Link>
          {welcomeLinks.github && (
            <a className="welcome-button secondary" href={welcomeLinks.github}>
              {c.hero.github}
              <ArrowRight size={18} />
            </a>
          )}
        </div>
        <div className="postmark" aria-hidden="true">
          <Dove size={37} />
          <span>{c.hero.stamp}</span>
        </div>
      </div>
      <CodePanel hero />
    </section>
  );
}
function HowItWorks() {
  return (
    <section id="how" className="welcome-section how-section">
      <div className="welcome-container">
        <div className="section-intro">
          <p className="eyebrow">{c.how.eyebrow}</p>
          <h2>{c.how.title}</h2>
          <p>{c.how.description}</p>
        </div>
        <div
          className="route-diagram"
          role="img"
          aria-label={`${c.how.nodes.join(" to ")}. ${c.how.return}.`}
        >
          <svg viewBox="0 0 1100 210" preserveAspectRatio="none" aria-hidden="true">
            <path className="route-path" d="M110 70 H990" />
            <path className="return-path" d="M990 160 H110" />
            <path
              d="m980 60 12 10-12 10M120 150l-12 10 12 10"
              fill="none"
              stroke="currentColor"
              strokeWidth="3"
            />
          </svg>
          <div className="route-nodes">
            {c.how.nodes.map((node, i) => (
              <div className="route-node" key={node}>
                <span className="route-number">0{i + 1}</span>
                <strong>{node}</strong>
              </div>
            ))}
          </div>
          <div className="route-return">{c.how.return}</div>
        </div>
      </div>
    </section>
  );
}
function Features() {
  return (
    <section id="features" className="welcome-section">
      <div className="welcome-container">
        <div className="section-intro">
          <p className="eyebrow">{c.features.eyebrow}</p>
          <h2>{c.features.title}</h2>
        </div>
        <div className="features-grid">
          {c.features.items.map((item, i) => (
            <article className="feature-card" key={item.title}>
              <span className="feature-index">{String(i + 1).padStart(2, "0")}</span>
              <h3>{item.title}</h3>
              <p>{item.text}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
function HonestStatus() {
  return (
    <section className="welcome-section status-section">
      <div className="welcome-container status-grid">
        <div className="section-intro">
          <p className="eyebrow">{c.status.eyebrow}</p>
          <h2>{c.status.title}</h2>
          <p>{c.status.description}</p>
        </div>
        <ol className="status-list">
          {c.status.states.map((item, i) => (
            <li key={item.title}>
              <span className="status-dot">{i + 1}</span>
              <div>
                <h3>{item.title}</h3>
                <p>{item.text}</p>
              </div>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}
function Developer() {
  return (
    <section className="welcome-section developer-section">
      <div className="welcome-container developer-grid">
        <div className="section-intro">
          <p className="eyebrow">{c.developer.eyebrow}</p>
          <h2>{c.developer.title}</h2>
          <p>{c.developer.description}</p>
          {welcomeLinks.docs && (
            <a className="text-link" href={welcomeLinks.docs}>
              {c.developer.guide}
              <ArrowRight size={17} />
            </a>
          )}
        </div>
        <CodePanel />
      </div>
    </section>
  );
}
function SelfHosted() {
  return (
    <section className="welcome-section hosted-section">
      <div className="welcome-container hosted-grid">
        <div className="section-intro">
          <p className="eyebrow">{c.selfHost.eyebrow}</p>
          <h2>{c.selfHost.title}</h2>
          <p>{c.selfHost.description}</p>
          {welcomeLinks.github && (
            <a className="welcome-button secondary" href={welcomeLinks.github}>
              {c.selfHost.github}
              <ArrowRight size={17} />
            </a>
          )}
        </div>
        <div className="quick-card">
          <span>{c.selfHost.quick}</span>
          <pre>
            <code>{c.compose}</code>
          </pre>
        </div>
      </div>
    </section>
  );
}
function FAQ() {
  return (
    <section className="welcome-section faq-section">
      <div className="welcome-container faq-grid">
        <div className="section-intro">
          <p className="eyebrow">{c.faq.eyebrow}</p>
          <h2>{c.faq.title}</h2>
        </div>
        <div className="faq-list">
          {c.faq.items.map((item) => (
            <details key={item.question}>
              <summary>
                {item.question}
                <ChevronDown size={18} />
              </summary>
              <p>{item.answer}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  );
}
function Footer() {
  return (
    <footer className="welcome-footer">
      <div className="welcome-container footer-grid">
        <div>
          <div className="welcome-brand">
            <Dove size={32} />
            <span>{c.brand}</span>
          </div>
          <p>{c.footer.description}</p>
        </div>
        <nav aria-label="Footer">
          {welcomeLinks.docs && <a href={welcomeLinks.docs}>{c.nav.docs}</a>}
          {welcomeLinks.github && <a href={welcomeLinks.github}>{c.nav.github}</a>}
          <Link to="/login">{c.nav.signIn}</Link>
        </nav>
        <span className="footer-origin">{c.footer.origin}</span>
      </div>
    </footer>
  );
}
export default function Welcome() {
  const { user } = useAuth();
  useEffect(() => {
    document.title = `${c.brand} — ${c.hero.title}`;
  }, []);
  if (user) return <Navigate to="/dashboard" replace />;
  return (
    <div className="welcome-page">
      <Navigation />
      <main>
        <Hero />
        <HowItWorks />
        <Features />
        <HonestStatus />
        <Developer />
        <SelfHosted />
        <FAQ />
      </main>
      <Footer />
    </div>
  );
}
