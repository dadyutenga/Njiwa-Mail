# System Analysis & Architecture Audit

## Phase 0: Source Type and Domain Analysis

### Source Stack Identification
- **Build Tool & Bundler**: Vite 5.4.19 with `@vitejs/plugin-react-swc` (SWC-based fast React compiler).
- **Core Framework**: React 18.3.1 with React DOM 18.3.1.
- **Language**: TypeScript 5.8.3. The original `tsconfig.app.json` had `"strict": false` and `"noImplicitAny": false`, which will be modernized to full TypeScript strict mode (`"strict": true`).
- **Routing**: React Router DOM 6.30.1 configured using JSX declarative routes (`<BrowserRouter>`, `<Routes>`, `<Route>`) in `src/App.tsx`. Eager loading was used for all pages.
- **State Management**:
  - Client state: React Context API (`AuthContext`, `LanguageContext`, `ThemeContext`) combined with component-level `useState`/`useReducer`.
  - Server state: TanStack React Query (`@tanstack/react-query` 5.83.0), configured in `App.tsx` with default retry/stale parameters.
  - Storage: `js-cookie` and `localStorage` handled via `src/lib/authStorage.ts`.
- **Styling Architecture**:
  - Tailwind CSS 3.4.17 with PostCSS and Autoprefixer.
  - Design tokens structured via HSL CSS variables in `src/index.css` supporting light and dark modes (`.dark`).
  - Animation utilities via `tailwindcss-animate` and typography via `@tailwindcss/typography`.
  - Iconography: `lucide-react` (v0.462.0).
- **UI & Component Primitives**:
  - Radix UI unstyled primitives (`@radix-ui/react-*`) mapped to shadcn/ui components (`accordion`, `alert-dialog`, `avatar`, `badge`, `button`, `calendar`, `card`, `dialog`, `dropdown-menu`, `popover`, `select`, `sheet`, `table`, `tabs`, `tooltip`, etc.).
  - Notification / Toast: `sonner` and Radix `toast`.
  - Command palette: `cmdk`.
  - Charts: `recharts` 2.15.4 with responsive containers.
  - Mobile bottom sheets: `vaul`.
- **Form Handling & Validation**:
  - React Hook Form 7.61.1 with `@hookform/resolvers/zod`.
  - Schema definition and validation: Zod 3.25.76.
- **Package Manager & Lockfiles**:
  - Active: `npm` with `package-lock.json` (lockfileVersion 3).
  - Unused / redundant artifacts: `bun.lockb` and `pnpm-lock.yaml` were present in the source root.
- **Test Setup**:
  - No automated test runner, test framework, or test scripts were present in `package.json`.
  - Ad-hoc manual testing files: `test-auth.html`, `test-upload.sh`, and `public/test-storage.html`.

### Business Domain & Domain Vocabulary List
- **Business Domain**: Academic and student campus file-sharing, timetable scheduling, peer networking (friends/classmates), campus community channels, and student marketplace platform.
- **Original Brand**: "Digital School Bag" (campus file-sharing platform).
- **Domain Vocabulary List** (to be completely scrubbed or generalized in Phase 4):
  - **Brand & Platform**: `schoolbag`, `digital school bag`, domain hosts, and domain URLs.
  - **Academic / School**: `school`, `university`, `college`, `student`, `lecturer`, `teacher`, `professor`, `class`, `classes`, `classmate`, `classmates`, `course`, `subject`, `study`, `study materials`, `examination`, `exam`, `timetable`, `semester`, `term`.
  - **Files & Storage**: `upload`, `uploader`, `chunked upload`, `files`, `my files`, `folder`, `sharing`, `shared with me`, `public share`, `storage`, `quota`, `trash`, `recycle bin`, `resource`, `share resource`.
  - **Social & Community**: `friend`, `friends`, `buddy`, `network`, `channel`, `public register`, `broadcast`.
  - **Marketplace & Commerce**: `marketplace`, `listing`, `listings`, `seller`, `buyer`, `product`, `item condition`, `price`, `contact seller`.
  - **Hardware / Specialized integrations**: `qr scanner`, `html5-qrcode`, `qrcode.react`, `ics export`, `ical`, `device token`, `otp verification`.

---

## Phase 1: Audit and Classification

### Classification Criteria
- **KEEP**: Components and utilities that are already generic UI primitives (buttons, inputs, dialogs, dropdowns, tables, toasts, loaders, theme switcher) and do not contain business-domain logic.
- **GENERALIZE**: Structural layouts, data tables, detail views, dashboard cards, auth screens, settings, and forms that provide good layout/UX structure but currently contain hardcoded domain copy, domain types, or domain services. These will be transformed into reusable, prop-driven, neutral components and pages.
- **DELETE**: Pure domain business logic, domain-specific REST services, domain mocks, hardware/scanner integrations, unused marketing kits, proprietary assets, and domain deployment scripts.

### Full Inventory & Classification Table

| Path | Classification | Role / Function | Modernization / Transformation Plan |
| :--- | :--- | :--- | :--- |
| `src/App.tsx` | GENERALIZE | Root application router and provider tree | Migrate to `createBrowserRouter`, lazy route loading, and wrap in `AppShell` |
| `src/main.tsx` | GENERALIZE | Entry point | Connect React 18 `createRoot`, initialize QueryClient, ThemeProvider, LanguageProvider |
| `src/index.css` | GENERALIZE | Core Tailwind and CSS design tokens | Retain clean HSL color system and radius variables; remove domain fonts/classes |
| `src/vite-env.d.ts` | KEEP | Vite type definitions | Retain and enhance with typed `ImportMetaEnv` |
| **Contexts & Providers** | | | |
| `src/contexts/AuthContext.tsx` | GENERALIZE | Authentication provider and state | Generalize to neutral user mock/session, role-based auth, no domain endpoints |
| `src/contexts/ThemeContext.tsx` | KEEP | Theme provider (light/dark/system) | Retain with localStorage persistence and system theme detection |
| `src/contexts/LanguageContext.tsx` | GENERALIZE | i18n context (English + Swahili) | Modernize and standardize with generic application dictionary keys |
| **Hooks** | | | |
| `src/hooks/use-mobile.tsx` | KEEP | Responsive mobile breakpoint detector | Keep as `useIsMobile` / `useMediaQuery` |
| `src/hooks/use-toast.ts` | KEEP | Radix toast hook | Keep for toast feedback integration |
| `src/hooks/usePageTitle.ts` | GENERALIZE | Document title manager | Generalize with neutral default app title |
| **Library & Utilities** | | | |
| `src/lib/api.ts` | GENERALIZE | Axios client instance and interceptors | Create generic API client with base URL from `import.meta.env`, error normalization |
| `src/lib/authStorage.ts` | GENERALIZE | Token and session persistence | Generalize to neutral auth storage utility (localStorage + fallback) |
| `src/lib/utils.ts` | KEEP | `cn` (clsx + tailwind-merge) utility | Retain and extend with formatters (date, currency, bytes, number) |
| `src/lib/translations.ts` | GENERALIZE | Translation strings (EN / SW) | Generalize strings to neutral app terminology (Dashboard, Settings, Records, etc.) |
| `src/lib/ics.ts` | DELETE | iCalendar (.ics) generation | Delete domain calendar generator |
| `src/lib/media.ts` | DELETE | Domain media preview helper | Generalize into generic file preview and delete domain endpoints |
| **Layout Components** | | | |
| `src/components/layout/Sidebar.tsx` | GENERALIZE | Navigation sidebar | Transform into collapsible AppShell sidebar driven by `routes.config.ts` |
| `src/components/layout/Navbar.tsx` | GENERALIZE | Header topbar | Generalize to topbar with breadcrumbs, command palette trigger, theme toggle, user menu |
| `src/components/layout/DashboardNav.tsx` | GENERALIZE | Dashboard navigation wrapper | Consolidate into `DashboardLayout` |
| `src/components/layout/BottomNav.tsx` | GENERALIZE | Mobile bottom navigation | Include in responsive mobile drawer / bottom navigation |
| `src/components/layout/InfoLayout.tsx` | GENERALIZE | Public / informational layout | Generalize into clean `AuthLayout` / public shell |
| `src/components/NavLink.tsx` | KEEP | Active route link wrapper | Keep as navigation helper |
| `src/components/NavigationIndicator.tsx`| KEEP | Route change progress bar | Retain for route transition feedback |
| `src/components/ProtectedRoute.tsx` | GENERALIZE | Route authorization guard | Generalize to support role-based permission checks and redirect parameters |
| `src/components/FilePreview.tsx` | GENERALIZE | File preview modal | Generalize to neutral document/image modal viewer |
| `src/components/QrScanner.tsx` | DELETE | HTML5 QR camera scanner | Delete domain hardware integration and scanner library |
| `src/components/library/ShareResourceForm.tsx` | DELETE | Domain resource sharing form | Delete domain form |
| `src/components/dashboard/DashboardHeader.tsx` | GENERALIZE | Dashboard header bar | Generalize into generic page header with title, description, and action slot |
| **UI Primitives (`src/components/ui/`)** | | | |
| `accordion.tsx` | KEEP | Radix accordion | Keep UI primitive |
| `alert-dialog.tsx` | KEEP | Radix alert dialog | Keep UI primitive; use for `ConfirmDialog` |
| `alert.tsx` | KEEP | Alert banner primitive | Keep UI primitive |
| `aspect-ratio.tsx` | KEEP | Aspect ratio container | Keep UI primitive |
| `avatar.tsx` | KEEP | Avatar with fallback | Keep UI primitive |
| `badge.tsx` | KEEP | Status badge primitive | Keep UI primitive |
| `breadcrumb.tsx` | KEEP | Breadcrumb navigation | Keep UI primitive |
| `button.tsx` | KEEP | Button component with variants | Keep UI primitive; add `IconButton` variant/wrapper |
| `calendar.tsx` | KEEP | React Day Picker calendar | Keep UI primitive |
| `card.tsx` | KEEP | Card container | Keep UI primitive |
| `carousel.tsx` | KEEP | Embla carousel wrapper | Keep UI primitive |
| `chart.tsx` | KEEP | Recharts styled wrapper | Keep data visualization primitive |
| `checkbox.tsx` | KEEP | Radix checkbox | Keep UI primitive |
| `collapsible.tsx` | KEEP | Radix collapsible | Keep UI primitive |
| `command.tsx` | KEEP | cmdk command dialog | Keep UI primitive for Command Palette |
| `context-menu.tsx` | KEEP | Context menu | Keep UI primitive |
| `dialog.tsx` | KEEP | Radix dialog | Keep UI primitive (Modal) |
| `drawer.tsx` | KEEP | Vaul drawer | Keep UI primitive (Drawer) |
| `dropdown-menu.tsx` | KEEP | Radix dropdown menu | Keep UI primitive |
| `form.tsx` | KEEP | React Hook Form wrapper | Keep form primitive |
| `hover-card.tsx` | KEEP | Hover preview card | Keep UI primitive |
| `input-otp.tsx` | KEEP | OTP input | Keep UI primitive |
| `input.tsx` | KEEP | Text input | Keep UI primitive |
| `label.tsx` | KEEP | Form label | Keep UI primitive |
| `menubar.tsx` | KEEP | Desktop menubar | Keep UI primitive |
| `navigation-menu.tsx` | KEEP | Navigation menu | Keep UI primitive |
| `pagination.tsx` | KEEP | Pagination controls | Keep UI primitive |
| `popover.tsx` | KEEP | Radix popover | Keep UI primitive |
| `progress.tsx` | KEEP | Progress bar | Keep UI primitive |
| `radio-group.tsx` | KEEP | Radio buttons | Keep UI primitive |
| `resizable.tsx` | KEEP | Resizable panels | Keep UI primitive |
| `scroll-area.tsx` | KEEP | Custom scrollbar container | Keep UI primitive |
| `select.tsx` | KEEP | Radix select dropdown | Keep UI primitive |
| `separator.tsx` | KEEP | Divider line | Keep UI primitive |
| `sheet.tsx` | KEEP | Slide-over drawer sheet | Keep UI primitive |
| `sidebar.tsx` | KEEP | shadcn sidebar implementation | Keep UI primitive |
| `skeleton.tsx` | KEEP | Loading skeleton | Keep UI primitive |
| `slider.tsx` | KEEP | Range slider | Keep UI primitive |
| `sonner.tsx` | KEEP | Sonner toaster | Keep feedback primitive |
| `switch.tsx` | KEEP | Toggle switch | Keep UI primitive |
| `table.tsx` | KEEP | Styled HTML table | Keep UI primitive |
| `tabs.tsx` | KEEP | Tabbed interface | Keep UI primitive |
| `textarea.tsx` | KEEP | Text area input | Keep UI primitive |
| `toast.tsx` | KEEP | Toast component | Keep UI primitive |
| `toaster.tsx` | KEEP | Toast container | Keep UI primitive |
| `toggle-group.tsx` | KEEP | Toggle group buttons | Keep UI primitive |
| `toggle.tsx` | KEEP | Toggle button | Keep UI primitive |
| `tooltip.tsx` | KEEP | Hover tooltip | Keep UI primitive |
| `use-toast.ts` | KEEP | Toast state management | Keep helper |
| **Pages & Screens** | | | |
| `src/pages/Dashboard/` | GENERALIZE | Dashboard with stat cards & activity | Transform into generic Dashboard with KPI cards, charts, activity feed, quick actions |
| `src/pages/Login/` | GENERALIZE | Login screen | Modern generic Login page with RHF + Zod, password toggle, mock login |
| `src/pages/Signup/` | GENERALIZE | Registration screen | Modern generic Register page with RHF + Zod |
| `src/pages/ForgotPassword/` | GENERALIZE | Password recovery start | Modern generic Forgot Password page |
| `src/pages/PasswordResetRequest/` | GENERALIZE | Password reset email form | Consolidate into clean Forgot/Reset password flow |
| `src/pages/PasswordResetVerify/` | GENERALIZE | OTP reset confirmation | Generalize to neutral verification code flow |
| `src/pages/PasswordResetConfirm/` | GENERALIZE | Set new password | Generalize to neutral Reset Password page |
| `src/pages/Profile/` | GENERALIZE | User profile & password change | Generalize to generic Profile page (account, avatar, password) |
| `src/pages/Settings/` | GENERALIZE | Application settings | Generalize to Settings page (account, appearance, notifications) |
| `src/pages/Classes/` | GENERALIZE | Academic class list | Transform into generic DataTable CRUD list page (CRUD example) |
| `src/pages/ClassDetails/` | GENERALIZE | Academic class details & students | Transform into generic Detail page (header, status badge, tabs, metadata, timeline) |
| `src/pages/Storage/` | GENERALIZE | Storage breakdown & stats | Generalize into Reports/Analytics page (date range + charts + export) |
| `src/pages/NotFound/` | GENERALIZE | 404 page | Modernize with consistent layout and back/home navigation |
| `src/pages/Index/` | GENERALIZE | Landing page | Transform into clean, neutral template landing / entry page |
| `src/pages/Files/` | DELETE | Domain file browser | Replace with generic CRUD table and file upload component |
| `src/pages/Upload/` | DELETE | Domain upload form | Replace with generic `FileUpload` primitive |
| `src/pages/Schedule/` | DELETE | Domain timetable & calendar | Extract calendar utilities; replace with generic components |
| `src/pages/Friends/` | DELETE | Student buddy list | Delete domain networking page |
| `src/pages/Marketplace/` | DELETE | Campus marketplace | Delete domain commerce page |
| `src/pages/Library/` | DELETE | Student library resources | Delete domain library page |
| `src/pages/Channel/` | DELETE | Broadcast channel | Delete domain channel page |
| `src/pages/Trash/` | DELETE | Recycle bin file restore | Delete domain storage trash page |
| `src/pages/About/` | DELETE | Legacy marketing about page | Delete domain content |
| `src/pages/HowToUse/` | DELETE | Legacy student tutorial page | Delete domain content |
| `src/pages/Privacy/` | DELETE | Legacy privacy policy page | Delete domain content |
| `src/pages/Notifications/` | GENERALIZE | Notifications list | Generalize into generic Notifications center & NotificationBell |
| **Services (`src/services/`)** | | | |
| `authService.ts` | DELETE | Domain auth REST endpoints | Replace with generic mock auth service and typed interfaces |
| `channelService.ts` | DELETE | Domain channel REST endpoints | Delete domain service |
| `classService.ts` | DELETE | Domain class REST endpoints | Delete domain service |
| `classmatesService.ts` | DELETE | Domain classmate REST endpoints | Delete domain service |
| `fileService.ts` | DELETE | Domain file REST endpoints | Delete domain service |
| `friendService.ts` | DELETE | Domain friend REST endpoints | Delete domain service |
| `friendsService.ts` | DELETE | Domain friends REST endpoints | Delete domain service |
| `libraryService.ts` | DELETE | Domain library REST endpoints | Delete domain service |
| `marketplaceService.ts`| DELETE | Domain marketplace REST endpoints | Delete domain service |
| `notificationService.ts`| DELETE | Domain notification REST endpoints | Replace with generic notification mock state |
| `scheduleService.ts` | DELETE | Domain schedule REST endpoints | Delete domain service |
| `searchService.ts` | DELETE | Domain search REST endpoints | Replace with generic client-side command palette filter |
| `sharingService.ts` | DELETE | Domain file sharing endpoints | Delete domain service |
| `storageService.ts` | DELETE | Domain storage quota endpoints | Delete domain service |
| `uploadService.ts` | DELETE | Chunked file upload endpoints | Delete domain upload service |
| **Assets & External Files** | | | |
| `src/assets/hero-education.jpg` | DELETE | Domain stock photo | Delete domain asset |
| `src/assets/hero-photo.jpeg` | DELETE | Domain stock photo | Delete domain asset |
| `public/Legacy Design System/` | DELETE | Static brand previews & mockups | Delete 25+ domain branding files |
| `public/favicon/` & `public/*.png` | GENERALIZE | Domain logos & icons | Replace with clean, neutral SVG/favicon |
| `deploy.sh`, `test-upload.sh` | DELETE | Netlify and upload shell scripts | Delete domain scripts |
| `fix_dark_mode*.py` | DELETE | Python dark mode search/replace | Delete obsolete helper scripts |
| `test-auth.html` | DELETE | Standalone HTML test | Delete ad-hoc test |
| `NETLIFY_DEPLOYMENT.md` | DELETE | Netlify deployment documentation | Delete domain doc |
| `UPLOAD_CONFIGURATION.md` | DELETE | Domain upload documentation | Delete domain doc |
| `bun.lockb`, `pnpm-lock.yaml` | DELETE | Redundant lockfiles | Delete unused lockfiles; retain `package-lock.json` |

---

## State and Data Flow Architecture

```
User Action / Browser Event
            │
            ▼
   React Router 6 (Data Router: createBrowserRouter)
            │
   ┌────────┴────────┐
   ▼                 ▼
AuthLayout      DashboardLayout (AppShell)
(Public Pages)  ├── Collapsible Sidebar (driven by routes.config.ts)
                ├── Topbar (Breadcrumbs, Search, ThemeToggle, UserMenu)
                ├── Content Area (Suspense / PageLoader)
                └── Footer
                     │
                     ▼
           Feature / Page Components
     (Dashboard, CRUD List, Detail, Reports, Settings)
            │                  │
            ▼                  ▼
    TanStack React Query     Client Contexts & Stores
   (useQuery / useMutation)   ├── AuthContext (User, Role, Session)
            │                 ├── ThemeContext (Light/Dark/System)
            ▼                 └── LanguageContext (i18n strings)
    Generic API Client                │
  (Axios with interceptors,           ▼
   Base URL, Error handler)   localStorage / Cookie
```

## Dead Code & Dependencies to Prune
1. `html5-qrcode` & `qrcode.react`: Domain-specific camera/QR scanner dependencies.
2. `lovable-tagger`: Development-time DOM tagging tool from previous builder.
3. `vite-plugin-pwa` & `workbox-window`: Overly complex service worker configuration for an app template; can be removed or kept minimal.
4. Ad-hoc scripts (`fix_dark_mode.py`, `deploy.sh`, `test-upload.sh`).
5. Redundant lockfiles (`bun.lockb`, `pnpm-lock.yaml`).
6. All 14 domain service files in `src/services/`.
