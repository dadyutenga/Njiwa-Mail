# MIGRATION NOTES

> [!WARNING]
> **License Notice**: The original source repository contained no `LICENSE` file. Verify license clearance or obtain appropriate rights before distributing or publishing software derived from retained code.

---

## 1. Secrets & Credentials Audit

A deep text search was executed across the entire original repository:

- No hardcoded API keys, bearer tokens, AWS/GCP credentials, database passwords, or private keys were found.
- All environment variables use the standard Vite prefix `VITE_` and are documented in `.env.example`.
- Reminder: Vite environment variables starting with `VITE_` are bundled into client-side JavaScript and exposed to the browser. Never store secret credentials in them.

---

## 2. Dependencies & Stack Modernization

- **React**: Updated to React 19.3.1 (`react`, `react-dom`).
- **Build Tool**: Updated to Vite 8.3.5 (`@vitejs/plugin-react` v6.2.1).
- **TypeScript**: Strict mode enabled on TypeScript 5.8.3.
- **Router**: Replaced TanStack Router with React Router 7 (`react-router-dom` 7.18.5) utilizing `createBrowserRouter`, lazy route loading, and route suspense.
- **Data Fetching**: Added TanStack Query v5 (`@tanstack/react-query` 5.83.0).
- **Forms & Validation**: Standardized on `react-hook-form` 7.71.2 + `zod` 3.24.2 + `@hookform/resolvers` 5.2.2.
- **Styling**: Tailwind CSS v4 (`@tailwindcss/vite`, `tailwindcss` 4.2.1) with centralized CSS custom properties in `src/styles/globals.css`.
- **Testing**: Added Vitest 4 + Testing Library (`@testing-library/react` 16, `@testing-library/jest-dom` 6, `jsdom` 27).
- **Linting & Code Quality**: Migrated to ESLint 9 flat configuration (`eslint.config.js`) + Prettier 3.

---

## 3. Items Removed

- **Domain Datasets**:
  - `src/data/Oil-data/*` (10 monthly fuel price JSON datasets).
  - `src/data/agriculture/*` (agricultural produce wholesale and retail pricing tables).
  - `src/lib/fuel-data.json`, `src/lib/bei-data.ts`, `src/lib/markets-data.ts`.
- **Domain Imagery**:
  - `src/assets/crops/*` (beans, bulrush millet, finger millet, maize, potatoes, rice, sorghum).
  - `src/assets/markets/*` (cattle, chicken, fuel, fx, goat).
  - `public/favicon.svg` (replaced with neutral template icon).
- **Obsolete Code & Samples**:
  - `public/sample/Bei.jsx` (standalone legacy demo).
  - `src/routeTree.gen.ts` (TanStack Router generated file).
  - Legacy deployment files (`wrangler.jsonc`, `netlify.toml`).
  - `src/hooks/usePreferredCategories.ts` and `src/components/CategoryBackLink.tsx`.

---

## 4. Components Generalized

- **Application Shell**: Converted into `src/components/layout/DashboardLayout.tsx` featuring a collapsible desktop sidebar, mobile drawer sheet, breadcrumbs, search command dialog, theme toggle, language switcher, notification popover, and user dropdown menu.
- **Dashboard View**: Replaced commodity metrics with generic KPI cards (`StatCard`, `TrendIndicator`), Recharts trend visualization (`AreaChartView`), quick action cards, and a recent activity log.
- **CRUD Records View**: Generalized into `src/features/example-crud/RecordsView.tsx` with sortable columns, full-text search, column visibility toggle, pagination, batch selection, CSV export, and create/edit modal dialog (`RecordFormModal`).
- **Reports & Analytics**: Replaced fuel/price charts with `src/features/reports/ReportsView.tsx` supporting date range filtering, multiple chart views (Bar, Line, Pie), validation, and CSV export.
- **Authentication**: Added clean authentication screens (`/login`, `/register`, `/forgot-password`, `/reset-password`) with React Hook Form + Zod validation.
- **Localization**: Added optional bilingual support (`en` and `sw`) with live language switching.
- **Error Handling**: Implemented custom `Status` pages for `403`, `404`, and `500` HTTP status codes, plus a top-level route `ErrorBoundary`.
