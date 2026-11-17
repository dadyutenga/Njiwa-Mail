import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from "react";
import {
  Link,
  Navigate,
  NavLink,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
} from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { docsHome, welcomeLinks } from "@/features/welcome/config";
import {
  Check,
  ChevronsUpDown,
  ExternalLink,
  FileText,
  Globe,
  KeyRound,
  Mail,
  Mailbox,
  Menu,
  PanelLeftClose,
  PenSquare,
  Plus,
  Search,
  UserRoundCheck,
  type LucideIcon,
} from "lucide-react";
import { usePhrase } from "@/features/i18n/i18n";
import { LanguageSwitcher } from "@/features/i18n/LanguageSwitcher";
import { NotificationBell, ThemeToggle, UserMenu } from "@/components/shared/Toolbar";
import { BrandIcon } from "@/components/shared/BrandIcon";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { admin, AdminError } from "@/lib/admin-api";
import { useAuth } from "@/features/auth/auth";
import { cn } from "@/lib/utils";
import {
  rememberWorkspace,
  useProjects,
  useWorkspace,
  useWsKey,
  WorkspaceProvider,
} from "./context";
import { detailLabels, findNav, navGroups } from "./nav";
import { Empty, ErrorState, errorMessage, Field, inputClass, run } from "./ui";
import { ComposePanel, type ComposePrefill } from "./pages/Compose";

// ---------------------------------------------------------------- compose context

type ComposeCtx = { open: (prefill?: ComposePrefill) => void };
const ComposeContext = createContext<ComposeCtx>({ open: () => {} });
export const useCompose = () => useContext(ComposeContext);

// ---------------------------------------------------------------- new project

export function NewProjectDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const phrase = usePhrase();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const valid = /^[A-Za-z0-9_-]{1,64}$/.test(name);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!valid) return setError(phrase("Use 1–64 letters, digits, dashes or underscores."));
    setBusy(true);
    setError("");
    try {
      await admin.send("/projects", "POST", { name });
      await client.invalidateQueries({ queryKey: ["projects"] });
      onOpenChange(false);
      setName("");
      rememberWorkspace(name);
      navigate(`/p/${encodeURIComponent(name)}/setup`, { state: { created: true } });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{phrase("New project")}</DialogTitle>
        <DialogDescription>
          {phrase(
            "A project opens in Live. Choose Sandbox only when sending a test. You will land on its setup checklist.",
          )}
        </DialogDescription>
        <form onSubmit={submit} className="space-y-4">
          <Field
            label={phrase("Project name")}
            error={error}
            hint={phrase("Letters, digits, dashes or underscores. Used in links.")}
          >
            <input
              autoFocus
              className={inputClass}
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                setError(
                  e.target.value && !/^[A-Za-z0-9_-]{1,64}$/.test(e.target.value)
                    ? phrase("Use 1–64 letters, digits, dashes or underscores.")
                    : "",
                );
              }}
              placeholder="sacas"
            />
          </Field>
          <Button type="submit" disabled={busy || !valid}>
            {busy ? phrase("Creating…") : phrase("Create project")}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------- switcher

function useSegment() {
  const { pathname } = useLocation();
  const parts = pathname.split("/").filter(Boolean); // p, project, segment, ...
  return { segment: parts[2] ?? "dashboard", rest: parts.slice(2) };
}

function WorkspaceSwitcher({ compact }: { compact?: boolean }) {
  const phrase = usePhrase();
  const { project } = useWorkspace();
  const { segment } = useSegment();
  const navigate = useNavigate();
  const projects = useProjects();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [creating, setCreating] = useState(false);
  const go = (p: string) => {
    setOpen(false);
    setQ("");
    rememberWorkspace(p);
    navigate(`/p/${encodeURIComponent(p)}/${segment}`);
  };
  const list = (projects.data ?? []).filter((p) => p.name.toLowerCase().includes(q.toLowerCase()));
  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            className={cn(
              "flex w-full items-center gap-2 rounded-xl border bg-background px-3 py-2 text-left text-sm hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              compact && "justify-center px-0",
            )}
            aria-label={phrase("Switch project")}
          >
            {!compact && (
              <>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold">{project}</span>
                  <span className="block text-xs text-muted-foreground">
                    {phrase("Project workspace")}
                  </span>
                </span>
                <ChevronsUpDown size={16} className="text-muted-foreground" />
              </>
            )}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-2">
          <input
            className={inputClass}
            placeholder={phrase("Search projects…")}
            aria-label={phrase("Search projects")}
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <ul
            className="mt-2 max-h-64 overflow-auto"
            role="listbox"
            aria-label={phrase("Projects")}
          >
            {projects.isPending && (
              <li className="p-2 text-sm text-muted-foreground">{phrase("Loading…")}</li>
            )}
            {list.map((p) => (
              <li key={p.id}>
                <button
                  role="option"
                  aria-selected={p.name === project}
                  onClick={() => go(p.name)}
                  className="flex w-full items-center justify-between rounded-md px-2 py-2 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
                >
                  <span className="truncate">{p.name}</span>
                  {p.name === project && <Check size={14} />}
                </button>
              </li>
            ))}
            {!projects.isPending && !list.length && (
              <li className="p-2 text-sm text-muted-foreground">{phrase("No project matches.")}</li>
            )}
          </ul>
          <button
            onClick={() => {
              setOpen(false);
              setCreating(true);
            }}
            className="mt-1 flex w-full items-center gap-2 rounded-md border-t px-2 py-2 text-left text-sm font-medium text-primary hover:bg-muted"
          >
            <Plus size={14} /> {phrase("New project")}
          </button>
        </PopoverContent>
      </Popover>
      <NewProjectDialog open={creating} onOpenChange={setCreating} />
    </>
  );
}

// ---------------------------------------------------------------- sidebar

function Sidebar({ compact, onNavigate }: { compact?: boolean; onNavigate?: () => void }) {
  const { user } = useAuth();
  const phrase = usePhrase();
  const { to, api, info } = useWorkspace();
  const key = useWsKey();
  const compose = useCompose();
  const unread = useQuery({
    queryKey: key("unread"),
    queryFn: () => api.get<{ total: number }>("/inbox/unread"),
    refetchInterval: 30_000,
  });
  const checklist = useQuery({
    queryKey: key("checklist"),
    queryFn: () => api.get<{ complete: boolean }>("/checklist"),
  });
  return (
    <div className="space-y-4">
      <WorkspaceSwitcher compact={compact} />
      <Button
        className="w-full"
        onClick={() => {
          onNavigate?.();
          compose.open();
        }}
        aria-label={phrase("Compose")}
      >
        <PenSquare size={16} /> {!compact && phrase("Compose")}
      </Button>
      <nav aria-label={phrase("Main navigation")} className="space-y-4">
        {navGroups.map((g) => {
          const items = g.items
            .filter((i) => !(i.path === "setup" && checklist.data?.complete))
            .filter((i) => !i.operator || user?.installation_operator !== false)
            .filter((i) => i.feature !== "forms" || info?.forms_enabled)
            .filter((i) => i.feature !== "chat" || info?.chat_enabled)
            .filter((i) => i.external !== "docs" || !!welcomeLinks.docs);
          if (items.length === 0) return null;
          return (
            <div key={g.label}>
              {!compact && (
                <p className="mb-1 px-3 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  {phrase(g.label)}
                </p>
              )}
              <ul className="space-y-0.5">
                {items.map(({ path, label, icon: Icon, badge, external }) => (
                  <li key={path}>
                    {external === "docs" ? (
                      <a
                        href={docsHome()}
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={onNavigate}
                        title={compact ? phrase(label) : undefined}
                        className={cn(
                          "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground transition hover:bg-muted hover:text-foreground",
                          compact && "justify-center px-0",
                        )}
                      >
                        <Icon size={18} />
                        {!compact && <span className="flex-1">{phrase(label)}</span>}
                        {!compact && <ExternalLink size={14} aria-hidden />}
                      </a>
                    ) : (
                      <NavLink
                        to={to(path)}
                        onClick={onNavigate}
                        title={compact ? phrase(label) : undefined}
                        className={({ isActive }) =>
                          cn(
                            "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition",
                            isActive
                              ? "bg-primary/15 text-primary"
                              : "text-muted-foreground hover:bg-muted hover:text-foreground",
                            compact && "justify-center px-0",
                          )
                        }
                      >
                        <Icon size={18} />
                        {!compact && <span className="flex-1">{phrase(label)}</span>}
                        {!compact && badge === "unread" && !!unread.data?.total && (
                          <span
                            className="rounded-full bg-primary px-1.5 text-xs text-primary-foreground"
                            aria-label={`${unread.data.total} unread`}
                          >
                            {unread.data.total}
                          </span>
                        )}
                      </NavLink>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </nav>
    </div>
  );
}

// ---------------------------------------------------------------- global search

type Hit = { kind: string; id: string; label: string; detail: string };

const hitMeta: Record<string, { label: string; icon: LucideIcon }> = {
  message: { label: "Messages", icon: Mail },
  domain: { label: "Domains", icon: Globe },
  sender: { label: "Senders", icon: UserRoundCheck },
  template: { label: "Templates", icon: FileText },
  api_key: { label: "API keys", icon: KeyRound },
  mailbox: { label: "Mailboxes", icon: Mailbox },
};

const searchScopes = [
  "Pages",
  "Messages",
  "Domains",
  "Senders",
  "Templates",
  "API keys",
  "Mailboxes",
];

function GlobalSearch() {
  const phrase = usePhrase();
  const { api, to } = useWorkspace();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const [active, setActive] = useState(0);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen(true);
      }
      if (
        e.key === "/" &&
        !(e.target as HTMLElement).closest("input,textarea,select,[contenteditable]")
      ) {
        e.preventDefault();
        setOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const hits = useQuery({
    queryKey: ["search", api.base, debounced],
    queryFn: () => api.get<{ items: Hit[] }>(`/search?q=${encodeURIComponent(debounced)}`),
    enabled: open && debounced.length >= 2,
  });
  const pages = navGroups
    .flatMap((g) => g.items)
    .filter((i) => q && phrase(i.label).toLowerCase().includes(q.toLowerCase()));
  const target = (h: Hit) =>
    ({
      message: to(`messages/${h.id}`),
      domain: to(`domains/${h.id}`),
      sender: to("senders"),
      template: to(`templates?id=${h.id}`),
      api_key: to(`messages?api_key=${encodeURIComponent(h.id)}`),
      mailbox: to(`inbox?mailbox=${h.id}`),
    })[h.kind] ?? to("dashboard");
  const goTo = (path: string) => {
    setOpen(false);
    setQ("");
    navigate(path);
  };
  const rows = [
    ...pages.map((p) => ({
      key: "page-" + p.path,
      group: phrase("Pages"),
      icon: p.icon,
      label: phrase(p.label),
      detail: "",
      path: to(p.path),
    })),
    ...(hits.data?.items ?? []).map((h) => {
      const meta = hitMeta[h.kind];
      return {
        key: h.kind + h.id,
        group: phrase(meta?.label ?? h.kind),
        icon: meta?.icon ?? Search,
        label: h.label,
        detail: h.detail,
        path: target(h),
      };
    }),
  ];
  useEffect(() => setActive(0), [debounced, pages.length, hits.data]);
  const current = rows.length ? Math.min(active, rows.length - 1) : 0;
  const onSearchKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (!rows.length) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((i) => Math.min(rows.length - 1, i + 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(0, i - 1));
    } else if (e.key === "Enter" && rows[current]) {
      e.preventDefault();
      goTo(rows[current].path);
    }
  };
  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        onClick={() => setOpen(true)}
        aria-label={phrase("Search")}
        className="gap-2"
      >
        <Search size={17} />
        <span className="hidden text-muted-foreground lg:inline">{phrase("Search")}</span>
        <kbd className="hidden rounded border px-1 text-[10px] text-muted-foreground lg:inline">
          Ctrl K
        </kbd>
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-xl gap-0 overflow-hidden p-0">
          <DialogTitle className="sr-only">{phrase("Search")}</DialogTitle>
          <DialogDescription className="sr-only">
            {phrase(
              "Message ID or full recipient address, domains, senders, templates, API keys, mailboxes and pages.",
            )}
          </DialogDescription>
          <div className="flex items-center gap-3 border-b px-4 pr-12">
            <Search size={18} className="shrink-0 text-muted-foreground" aria-hidden />
            <input
              autoFocus
              className="h-14 w-full bg-transparent text-base outline-none placeholder:text-muted-foreground"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={onSearchKey}
              placeholder={phrase("Search pages, messages, and records")}
              aria-label={phrase("Search")}
              aria-activedescendant={rows[current] ? "search-" + rows[current].key : undefined}
            />
          </div>
          <ul className="flex flex-wrap gap-1.5 px-4 py-3" aria-label={phrase("Search covers")}>
            {searchScopes.map((scope) => (
              <li
                key={scope}
                className="rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground"
              >
                {phrase(scope)}
              </li>
            ))}
          </ul>
          <ul className="max-h-80 overflow-auto px-2 pb-2" aria-live="polite" role="listbox">
            {rows.map((row, i) => {
              const Icon = row.icon;
              const showGroup = i === 0 || rows[i - 1].group !== row.group;
              return (
                <li key={row.key} role="presentation">
                  {showGroup && (
                    <p className="px-2 pb-1 pt-2 text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                      {row.group}
                    </p>
                  )}
                  <button
                    id={"search-" + row.key}
                    role="option"
                    aria-selected={i === current}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm focus-visible:outline-none",
                      i === current ? "bg-primary/10" : "hover:bg-muted",
                    )}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => goTo(row.path)}
                  >
                    <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                      <Icon size={16} aria-hidden />
                    </span>
                    <span className="min-w-0">
                      <span className="block truncate font-medium">{row.label}</span>
                      {row.detail && (
                        <span className="block truncate text-xs text-muted-foreground">
                          {row.detail}
                        </span>
                      )}
                    </span>
                  </button>
                </li>
              );
            })}
            {q.trim().length > 0 && q.trim().length < 2 && pages.length === 0 && (
              <li className="px-3 py-6 text-center text-sm text-muted-foreground">
                {phrase(
                  "Type at least two characters to search records. A page name can be shorter.",
                )}
              </li>
            )}
            {debounced.length >= 2 && hits.isFetching && (
              <li className="px-3 py-2 text-sm text-muted-foreground">{phrase("Searching…")}</li>
            )}
            {debounced.length >= 2 && hits.data && !hits.data.items.length && !pages.length && (
              <li className="px-3 py-6 text-center text-sm text-muted-foreground">
                {phrase("Nothing found in this project and environment.")}
              </li>
            )}
            {!q.trim() && (
              <li className="px-3 py-6 text-center text-sm text-muted-foreground">
                {phrase(
                  "Search a page by name, or a message, domain, sender, template, key, or mailbox.",
                )}
              </li>
            )}
          </ul>
          <div className="flex items-center justify-between border-t px-4 py-2 text-xs text-muted-foreground">
            <span>{phrase("Arrow keys move. Enter opens.")}</span>
            <span>Esc</span>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

// ---------------------------------------------------------------- breadcrumbs

function Breadcrumbs() {
  const phrase = usePhrase();
  const { project, to } = useWorkspace();
  const { segment, rest } = useSegment();
  const found = findNav(segment);
  const detail = rest[1];
  return (
    <nav aria-label={phrase("Breadcrumb")} className="hidden min-w-0 sm:block">
      <ol className="flex items-center gap-1.5 truncate text-sm text-muted-foreground">
        <li>
          <Link to={to("dashboard")} className="hover:text-foreground">
            {project}
          </Link>
        </li>
        {found && (
          <>
            <li aria-hidden>/</li>
            <li>{phrase(found.group.label)}</li>
            <li aria-hidden>/</li>
            <li>
              {detail ? (
                <Link to={to(segment)} className="hover:text-foreground">
                  {phrase(found.item.label)}
                </Link>
              ) : (
                <span aria-current="page" className="text-foreground">
                  {phrase(found.item.label)}
                </span>
              )}
            </li>
          </>
        )}
        {!found && detailLabels[segment] && (
          <>
            <li aria-hidden>/</li>
            <li aria-current="page" className="text-foreground">
              {phrase(detailLabels[segment])}
            </li>
          </>
        )}
        {detail && (
          <>
            <li aria-hidden>/</li>
            <li aria-current="page" className="max-w-40 truncate text-foreground">
              {detailLabels[detail]
                ? phrase(detailLabels[detail])
                : decodeURIComponent(detail).slice(0, 12)}
            </li>
          </>
        )}
      </ol>
    </nav>
  );
}

// ---------------------------------------------------------------- shell

function Shell() {
  const phrase = usePhrase();
  const { project, to } = useWorkspace();
  const [compact, setCompact] = useState(false);
  const [mobile, setMobile] = useState(false);
  const [compose, setCompose] = useState<{
    open: boolean;
    prefill?: ComposePrefill;
    nonce: number;
  }>({ open: false, nonce: 0 });
  const composeCtx = useMemo<ComposeCtx>(
    () => ({ open: (prefill) => setCompose((c) => ({ open: true, prefill, nonce: c.nonce + 1 })) }),
    [],
  );
  useEffect(() => rememberWorkspace(project), [project]);
  // Screen readers and keyboard users: after navigating to another page, move focus
  // to the page content so the new page is announced and tabbing starts there.
  const { pathname: currentPath } = useLocation();
  const firstPath = useRef(currentPath);
  useEffect(() => {
    if (firstPath.current === currentPath) return;
    firstPath.current = currentPath;
    document.getElementById("main")?.focus({ preventScroll: true });
  }, [currentPath]);
  return (
    <ComposeContext.Provider value={composeCtx}>
      <div className="min-h-screen bg-background text-foreground">
        <a
          href="#main"
          className="sr-only focus:not-sr-only focus:fixed focus:left-2 focus:top-2 focus:z-50 focus:rounded focus:bg-background focus:p-2"
        >
          {phrase("Skip to content")}
        </a>
        <aside
          className={cn(
            "fixed inset-y-0 left-0 z-20 hidden overflow-y-auto border-r bg-card p-3 transition-all md:block",
            compact ? "w-20" : "w-64",
          )}
        >
          <Link to={to("dashboard")} className="mb-4 flex h-10 items-center gap-2.5 px-2 font-bold">
            <BrandIcon size={32} />
            {!compact && <span className="tracking-tight">Njiwa-Mail</span>}
          </Link>
          <Sidebar compact={compact} />
        </aside>
        <div className={compact ? "md:pl-20" : "md:pl-64"}>
          <header className="sticky top-0 z-10 flex h-14 items-center gap-2 border-b bg-background/95 px-3 backdrop-blur sm:px-6">
            <Sheet open={mobile} onOpenChange={setMobile}>
              <Button
                variant="ghost"
                size="icon"
                className="md:hidden"
                aria-label={phrase("Open navigation")}
                onClick={() => setMobile(true)}
              >
                <Menu />
              </Button>
              <SheetContent side="left" className="w-72 overflow-y-auto p-3">
                <SheetTitle className="mb-3 flex items-center gap-2">
                  <BrandIcon size={28} /> Njiwa-Mail
                </SheetTitle>
                <SheetDescription className="sr-only">
                  {phrase("Choose a page to navigate to.")}
                </SheetDescription>
                <Sidebar onNavigate={() => setMobile(false)} />
              </SheetContent>
            </Sheet>
            <Button
              variant="ghost"
              size="icon"
              className="hidden md:inline-flex"
              aria-label={phrase("Toggle sidebar")}
              aria-expanded={!compact}
              onClick={() => setCompact(!compact)}
            >
              <PanelLeftClose />
            </Button>
            <Breadcrumbs />
            <div className="ml-auto flex items-center gap-1">
              <GlobalSearch />
              <LanguageSwitcher />
              <ThemeToggle />
              <NotificationBell />
              <UserMenu />
            </div>
          </header>
          <main
            id="main"
            tabIndex={-1}
            className="mx-auto max-w-7xl p-4 outline-none sm:p-6 lg:p-8"
          >
            <Outlet />
          </main>
        </div>
        <ComposePanel
          key={compose.nonce}
          open={compose.open}
          prefill={compose.prefill}
          onOpenChange={(open) => setCompose((c) => ({ ...c, open }))}
        />
      </div>
    </ComposeContext.Provider>
  );
}

function NotFoundProject({ project }: { project: string }) {
  const phrase = usePhrase();
  return (
    <div className="grid min-h-screen place-items-center bg-background p-6 text-foreground">
      <div className="max-w-md rounded-2xl border bg-card p-8 text-center">
        <h1 className="text-2xl font-bold">{phrase("Project not found")}</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {phrase("There is no project named")} <strong>{project}</strong>{" "}
          {phrase("or you cannot open it. Check the link, or pick a project you can use.")}
        </p>
        <Link
          to="/app?choose=1"
          className="mt-6 inline-block rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground"
        >
          {phrase("Back to my projects")}
        </Link>
      </div>
    </div>
  );
}

/** Route element for the project workspace. Normal pages always use Live. */
export function WorkspaceRoute() {
  const { project = "" } = useParams();
  const env = "live";
  const info = useQuery({
    queryKey: ["ws", project, env, "context"],
    queryFn: () => admin.get(`/w/${encodeURIComponent(project)}/${env}/context`),
    retry: (n, e) => !(e instanceof AdminError && e.status === 404) && n < 1,
    staleTime: 30_000,
  });
  if (
    info.error instanceof AdminError &&
    info.error.status === 404 &&
    info.error.code === "project_not_found"
  )
    return <NotFoundProject project={project} />;
  if (info.error)
    return (
      <FirstRunShell>
        <ErrorState error={info.error} retry={() => info.refetch()} />
      </FirstRunShell>
    );
  return (
    <WorkspaceProvider project={project}>
      <Shell />
    </WorkspaceProvider>
  );
}

/** Preserve old environment URLs without restoring a global environment. */
export function LegacyWorkspaceRoute({ mode }: { mode: "live" | "sandbox" }) {
  const { project = "", "*": legacyPath = "" } = useParams();
  const rest = legacyPath.replace(/^sandbox(\/|$)/, "captures$1");
  const { search, hash } = useLocation();
  const params = new URLSearchParams(search);
  if (mode === "sandbox" && /^(messages|sent|scheduled|unknown|bulk|keys)(\/|$)/.test(rest))
    params.set("mode", "sandbox");
  return (
    <Navigate
      replace
      to={`/p/${encodeURIComponent(project)}/${rest || "dashboard"}${params.size ? "?" + params : ""}${hash}`}
    />
  );
}

/** Blank-state shell for the first-run screen (no projects yet). */
export function FirstRunShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex h-14 items-center gap-2 border-b px-4">
        <BrandIcon size={28} />
        <span className="font-bold">Njiwa-Mail</span>
        <div className="ml-auto flex items-center gap-1">
          <LanguageSwitcher />
          <ThemeToggle />
          <UserMenu />
        </div>
      </header>
      <main className="mx-auto max-w-3xl p-4 sm:p-8">{children}</main>
    </div>
  );
}

export { Empty };
