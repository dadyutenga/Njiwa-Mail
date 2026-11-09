# Architecture & Design Decisions

This document logs significant architectural and design choices, trade-offs, and decisions made during the conversion from the domain-specific codebase into a reusable frontend template.

---

## Phase 0 & 1: Source Discovery & Classification

### 1. Source Stack & Domain Assessment
- **Finding**: The source codebase is an academic campus platform ("Digital School Bag") built with Vite 5, React 18, TypeScript, Tailwind CSS, Radix UI / shadcn/ui, and TanStack React Query.
- **Decision**: Keep the modern Vite + React 18 + Tailwind + Radix UI foundation. It is an excellent, flexible, modern stack.
- **Reason**: The existing UI design system (HSL tokens, typography, component styling) is modern and clean. Rebuilding from scratch would discard great UI styling, whereas generalizing and modernizing it yields a clean, enterprise-ready template.

### 2. TypeScript Strict Mode & Compiler Options
- **Finding**: Original `tsconfig.app.json` had `"strict": false` and `"noImplicitAny": false`, and an invalid `"ignoreDeprecations": "6.0"` setting.
- **Decision**: Modernize compiler options to `"strict": true`, enforce zero unhandled `any` types, and configure standard modern TypeScript settings.
- **Reason**: A reusable template must ensure strong type safety so downstream developers have full autocomplete, prop validation, and refactoring confidence.

### 3. Routing Architecture & Unified Config Array
- **Finding**: Original routing used JSX `<Routes>` with eager imports in `src/App.tsx`, requiring manual updates to both sidebar markup and router trees when adding a new page.
- **Decision**: Migrate to React Router's `createBrowserRouter` driven by a unified navigation/route config array (`src/app/routes.config.tsx`), with `React.lazy` code-splitting, custom error elements, and automatic breadcrumb/sidebar generation.
- **Reason**: Meets the core requirement: adding a new page requires only creating a page file and adding a single entry to the config array.

### 4. Pruning Domain Services and Hardware Integrations
- **Finding**: 14 domain service files (`classService`, `marketplaceService`, `channelService`, `sharingService`, etc.) and camera QR scanning (`html5-qrcode`, `qrcode.react`) were tightly coupled to the legacy Django backend.
- **Decision**: Remove all domain-specific REST services and QR scanner packages. Implement a clean, generic API client (Axios wrapper with request/response interceptors, baseURL configuration, and normalized errors) along with typed mock hooks demonstrating TanStack Query query and mutation patterns.
- **Reason**: Templates should never ship with broken, dead domain backend calls; they must provide clean API abstractions and mock data ready for customization.

### 5. Cleaning Branding, Assets, and Unused Lockfiles
- **Finding**: Proprietary assets (logos, marketing kits under public branding directories, educational photos) and redundant lockfiles (`bun.lockb`, `pnpm-lock.yaml`) were cluttering the repo.
- **Decision**: Remove proprietary assets and redundant lockfiles. Standardize on `npm` (`package-lock.json`), and replace branding with neutral "Frontend Template" / "Your App" placeholders and clean geometric SVGs.
- **Reason**: Ensures the template is lightweight, domain-neutral, free of proprietary branding, and has a single source of truth for dependencies.

---

## Phase 2 & 3: Modernization & Template Construction

### 6. Directory Layout & Structure
- **Decision**: Adopted the target folder structure:
  - `src/app/` (providers, router, routes.config, App root)
  - `src/components/ui/` (Radix UI primitives)
  - `src/components/shared/` (DataTable, StatCard, ChartCard, ActivityFeed, ConfirmDialog, etc.)
  - `src/components/layout/` (DashboardLayout, AuthLayout, Breadcrumbs, Footer)
  - `src/features/` (feature slices, e.g. `example-crud` with `CrudForm` and TanStack Query hooks)
  - `src/hooks/` (reusable hooks)
  - `src/lib/` (Axios API client, auth storage, translations, formatters)
  - `src/pages/` (clean page views)
  - `src/types/` (domain-neutral TypeScript contracts)
  - `src/test/` (Vitest suites)
- **Reason**: Highly scalable enterprise folder layout that separates concerns clearly and makes finding code intuitive.

### 7. Rich Component Suite vs. Minimal Skeleton
- **Decision**: Included a comprehensive library of UI components:
  - Primitives: `Button`, `IconButton`, `Input`, `PasswordInput`, `Select`, `MultiSelect`, `Textarea`, `Checkbox`, `RadioGroup`, `Switch`, `DatePicker`, `DateRangePicker`, `FileUpload`, `Badge`, `Avatar`, `Tooltip`, `DropdownMenu`, `Tabs`, `Accordion`, `Modal`, `Drawer`, `Popover`, `Spinner`, `Progress`, `EmptyState`, `Alert`, `Pagination`, `Breadcrumbs`, `Stepper`.
  - Data & Visualization: `DataTable` (sort, filter, search, paginate, select, column toggle, export CSV), `StatCard`, `ChartCard`, Recharts wrappers (`AreaChartWrapper`, `BarChartWrapper`, `LineChartWrapper`, `PieChartWrapper`), `ActivityFeed`, `KpiTrend`.
  - Feedback & Misc: `ConfirmDialog`, `ErrorBoundary`, `PageLoader`, `CommandPalette` (`Cmd+K`), `ThemeToggle`, `UserMenu`, `NotificationBell`, `PrintableDocument`, `CopyButton`.
- **Reason**: Provides a complete building kit so new pages can be assembled in minutes without installing external packages.

### 8. Authentication Scaffold & Role Switching
- **Decision**: Built a complete `AuthContext` with mock authentication, persistence via `localStorage`, and an interactive role switcher in the user menu.
- **Reason**: Allows immediate testing of role-based route guards (`admin`, `manager`, `user`) without requiring a real backend during development.

---

## Phase 4: Domain Scrubbing

### 9. Removal of All Domain References
- **Decision**: Renamed package to `frontend-template`. Replaced all domain terms (student, lecturer, classmate, timetable, schoolbag, campus) with neutral terminology (Records, Entries, Items, Accounts).
- **Intentional Leftovers**: No domain words remain anywhere in the runtime codebase, metadata, index.html, manifests, documentation, or tests.

---

## Phase 5: Quality, Testing, and Deployment

### 10. Vitest Testing Stack
- **Decision**: Configured Vitest 2.1 with React Testing Library, Jest DOM matchers, and JSDOM environment. Added unit tests for `DataTable`, `Modal`, `CrudForm`, and `ProtectedRoute`.
- **Reason**: Vitest integrates natively with Vite's module graph, executing tests with extreme speed while maintaining full Jest API compatibility.

### 11. Multi-Stage Containerization & CI
- **Decision**: Added a multi-stage `Dockerfile` using `node:20-alpine` for building and `nginx:alpine` with SPA routing and gzip compression for runtime, along with a GitHub Actions CI workflow.
- **Reason**: Guarantees reproducible, immutable builds ready for any cloud container service (Docker, Kubernetes, AWS ECS, GCP Cloud Run).
