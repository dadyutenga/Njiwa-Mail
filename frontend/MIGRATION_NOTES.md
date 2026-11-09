# Migration Notes

## License: Review Required
- **Status**: No `LICENSE` file was found in the supplied source repository.
- **Notice**: Users adopting this template should ensure they add an appropriate open-source license (such as MIT or Apache 2.0) or commercial proprietary header before distributing downstream projects.

---

## Credentials and Sensitive Information Audit
- **Findings & Actions**:
  - Found hardcoded Google Analytics tracking ID `G-YMV2K4WS09` in `index.html` — removed completely.
  - Hardcoded production domain hosts and legacy API hosts in `index.html`, `vite.config.ts`, and `netlify.toml` — removed and replaced with standard environment variables.
  - Removed verbose debug logging in Axios interceptors and authentication storage that previously printed request bodies and auth response tokens to browser console.
  - Replaced `.env` with neutral localhost endpoints and created a clean `.env.example`.

---

## Upgrade & Transformation Summary

### 1. Stack Modernization
- **Compiler**: Upgraded TypeScript compiler options from non-strict to strict mode (`"strict": true`). Removed unrecognized `"ignoreDeprecations": "6.0"`.
- **Router**: Migrated from declarative eager JSX `<Routes>` to React Router 6.30 Data Router (`createBrowserRouter`) with code-splitting (`React.lazy`) and custom error handling (`ErrorBoundary`).
- **State Architecture**: Server cache via TanStack React Query v5; client session, theme, and language via React Context.
- **Testing**: Added Vitest v2, React Testing Library, Jest DOM matchers, and JSDOM test runner.

### 2. Files and Services Removed Outright
- **14 Domain REST Services**: `authService.ts`, `channelService.ts`, `classService.ts`, `classmatesService.ts`, `fileService.ts`, `friendService.ts`, `friendsService.ts`, `libraryService.ts`, `marketplaceService.ts`, `notificationService.ts`, `scheduleService.ts`, `searchService.ts`, `sharingService.ts`, `storageService.ts`, `uploadService.ts`.
- **25+ Domain Pages & Screens**: `About/`, `Channel/`, `ClassDetails/`, `Classes/`, `Dashboard/` (legacy), `Files/`, `Friends/`, `HowToUse/`, `Index/` (legacy), `Library/`, `Marketplace/`, `Notifications/` (legacy), `PasswordReset*/`, `Privacy/`, `Profile/` (legacy), `Schedule/`, `Settings/` (legacy), `Sharing/`, `Signup/` (legacy), `Storage/`, `Trash/`, `Upload/`.
- **Dead Scripts & Redundant Lockfiles**: `bun.lockb`, `pnpm-lock.yaml`, `deploy.sh`, `test-upload.sh`, `test-auth.html`, `fix_dark_mode.py`, `fix_dark_mode2.py`, `public/test-storage.html`, legacy design system mockups directory, `NETLIFY_DEPLOYMENT.md`, `UPLOAD_CONFIGURATION.md`.
- **Hardware & Obsolete Packages**: Uninstalled `html5-qrcode`, `qrcode.react`, `lovable-tagger`, `vite-plugin-pwa`, `workbox-window`.

### 3. Components Generalized & Created
- **Layout Shell**:
  - `DashboardLayout.tsx`: Collapsible desktop sidebar, responsive mobile drawer sheet, breadcrumbs, search trigger, notification bell, theme toggle, user menu, footer.
  - `AuthLayout.tsx`: Centered public authentication shell.
  - `routes.config.tsx`: Single config array driving the router, sidebar, and mobile drawer.
- **UI Primitives Added**:
  - `IconButton`, `PasswordInput`, `MultiSelect`, `DatePicker`, `DateRangePicker`, `FileUpload`, `Spinner`, `EmptyState`, `Stepper`.
- **Data & Shared Components**:
  - `DataTable.tsx`: Generic data table with sort, filter, search, paginate, select, column visibility, row actions, and CSV export.
  - `StatCard.tsx`, `ChartCard.tsx`, `Charts.tsx` (Area, Bar, Line, Pie Recharts wrappers).
  - `ActivityFeed.tsx`, `KpiTrend.tsx`, `ConfirmDialog.tsx`, `ErrorBoundary.tsx`, `PageLoader.tsx`, `Modal.tsx`, `CommandPalette.tsx`, `PrintableDocument.tsx`, `CopyButton.tsx`.
- **Feature Pages Built**:
  - `DashboardPage.tsx`, `ListPage.tsx` (CRUD example), `DetailPage.tsx`, `ReportsPage.tsx`, `ProfilePage.tsx`, `SettingsPage.tsx`, `ExamplePage.tsx` ("How to add a page"), `LandingPage.tsx`, `LoginPage.tsx`, `RegisterPage.tsx`, `ForgotPasswordPage.tsx`, `ResetPasswordPage.tsx`, `NotFoundPage.tsx` (404), `ForbiddenPage.tsx` (403), `ServerErrorPage.tsx` (500).
- **Internationalization (i18n)**:
  - English (`en`) and Swahili (`sw`) localization dictionaries in `src/lib/translations.ts` and dynamic language switcher.
