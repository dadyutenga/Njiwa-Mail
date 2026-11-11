# ANALYSIS: Source Stack Audit & Classification

## Phase 0: Detect Source Type and Business Domain

### Source Stack

- **Framework & Runtime**: React 19.2 (migrated to React 19.3.1), TypeScript 5.8, Vite 7 (upgraded to Vite 8.3.5 with `@vitejs/plugin-react`).
- **Routing**: Originally TanStack Start with file-based routing (`@tanstack/react-router`, `src/routes/*`, `src/routeTree.gen.ts`). Replaced with standard React Router 7 (`react-router-dom` v7.18.5) using `createBrowserRouter`, lazy-loaded page modules, route suspense fallback, and `errorElement`.
- **State Management**:
  - Original: Direct static JSON imports, local React component state (`useState`), and `localStorage` for category preferences.
  - Modernized: TanStack Query v5 for server state and asynchronous data, React Context for client authentication session (`AuthProvider`) and theme preferences (`ThemeProvider`), plus an optional localization context (`I18nProvider`).
- **Styling & Design Tokens**: Tailwind CSS v4 (`@tailwindcss/vite`, `tailwindcss` 4.2.1) with centralized CSS custom properties in `src/styles/globals.css` (defining dark and light tokens, radius, fonts, surface, and semantic colors).
- **UI Component Primitives**: Radix UI primitives (`@radix-ui/react-*`), Lucide React icons, Vaul drawer, Sonner toast notifications, Embla Carousel, and Recharts 2.15 data visualization.
- **Package Manager**: npm (with existing `package-lock.json`). Residual `bun.lockb` and `bunfig.toml` preserved from initial snapshot.
- **Testing Setup**: Originally none. Modernized to Vitest 4 + Testing Library (`@testing-library/react`, `@testing-library/jest-dom`, `jsdom`).

### Business Domain & Vocabulary List

The original codebase (`Bei-tanzania-ui-main`) was an agricultural and retail commodity market tracking platform for Tanzania ("Bei" means "Price" in Swahili). It displayed historical and current prices for farm produce, fuel, livestock, and foreign currency exchange rates across Tanzanian regions and markets.

#### Domain Vocabulary List (Scrubbed / Generalized):

- **Core Branding & Entities**: `Bei`, `Tanzania`, `TZS`, `Shilling`, `Dar es Salaam`, `Arusha`, `Dodoma`, `Mbeya`, `Mwanza`, `Zanzibar`.
- **Agricultural Produce / Crops**: `crop`, `crops`, `agriculture`, `wholesale`, `retail`, `maize`, `rice`, `beans`, `sorghum`, `bulrush millet`, `finger millet`, `wheat`, `potato`, `potatoes`, `cassava`, `onions`, `tomatoes`.
- **Energy & Commodities**: `fuel`, `oil`, `petrol`, `diesel`, `kerosene`, `EWURA`, `pump price`.
- **Livestock**: `livestock`, `cattle`, `chicken`, `goat`, `cow`, `bull`, `heifer`, `sheep`.
- **Finance & Market Terms**: `fx`, `forex`, `foreign exchange`, `exchange rate`, `buying rate`, `selling rate`, `market`, `markets`, `regional market`, `town`, `zone`.

All domain terms were scrubbed from application runtime code, configuration, comments, and public assets, replacing them with generic equivalents:

- "Products / Crops / Commodities" &rarr; "Records / Items"
- "Markets / Regions" &rarr; "Categories / Workspaces"
- "Price History / Fuel Prices" &rarr; "Reports / Activity Metrics"

---

## Phase 1: Audit and Classification

### Classification Summary Table

| Category       | Description                                                                                                     | Count |
| :------------- | :-------------------------------------------------------------------------------------------------------------- | :---- |
| **KEEP**       | Already generic reusable primitives (buttons, inputs, dialogs, drawers, badges, tables, layouts, hooks, utils). | 50+   |
| **GENERALIZE** | Rich layouts and components with domain logic scrubbed into data-driven, configurable template components.      | 12    |
| **DELETE**     | Hardcoded domain JSON datasets, crop/market imagery, brand favicons, and obsolete generated router files.       | 25    |

### Comprehensive File Classification

| File Path                                  | Classification | Rationale                                                                                  |
| :----------------------------------------- | :------------- | :----------------------------------------------------------------------------------------- |
| `src/assets/crops/*.jpg` (7 files)         | **DELETE**     | Domain-specific crop imagery (maize, rice, sorghum, beans, etc.).                          |
| `src/assets/markets/*.jpg` (5 files)       | **DELETE**     | Domain-specific market imagery (cattle, chicken, fuel, fx, goat).                          |
| `src/data/Oil-data/*.json` (10 files)      | **DELETE**     | Domain static fuel prices datasets (April, Feb, March, August, etc.).                      |
| `src/data/Oil-data/mockData.js`            | **DELETE**     | Domain fuel mock records.                                                                  |
| `src/data/Oil-data/realData.js`            | **DELETE**     | Domain fuel real records.                                                                  |
| `src/data/agriculture/**/*.json` (3 files) | **DELETE**     | Hardcoded agricultural produce wholesale/retail datasets.                                  |
| `src/data/agriculture/**/structure.md`     | **DELETE**     | Domain-specific documentation on crop pricing datasets.                                    |
| `src/lib/bei-data.ts`                      | **DELETE**     | Domain calculation and data access utilities for commodity prices.                         |
| `src/lib/fuel-data.json`                   | **DELETE**     | Domain dataset for regional fuel pump prices.                                              |
| `src/lib/markets-data.ts`                  | **DELETE**     | Domain dataset for Tanzanian regional market lists.                                        |
| `src/hooks/usePreferredCategories.ts`      | **DELETE**     | Domain category preferences hook coupled to commodity types.                               |
| `src/components/CategoryBackLink.tsx`      | **DELETE**     | Domain route backlink coupled to market categories.                                        |
| `src/routeTree.gen.ts`                     | **DELETE**     | Auto-generated TanStack Router route tree; superseded by React Router config.              |
| `public/favicon.svg`                       | **DELETE**     | Original domain-specific icon.                                                             |
| `public/sample/Bei.jsx`                    | **DELETE**     | Monolithic legacy domain sample script.                                                    |
| `src/components/AppShell.tsx`              | **GENERALIZE** | Domain layout generalized into `src/components/layout/DashboardLayout.tsx`.                |
| `src/routes/__root.tsx`                    | **GENERALIZE** | Root document layout generalized into `App.tsx` and `DashboardLayout.tsx`.                 |
| `src/routes/index.tsx`                     | **GENERALIZE** | Converted into generic `DashboardView` (stat cards, trend chart, quick actions, activity). |
| `src/routes/categories.tsx`                | **GENERALIZE** | Converted into generic list pattern (`src/features/example-crud/RecordsView.tsx`).         |
| `src/routes/crops.tsx`                     | **GENERALIZE** | Data-table and filter structure generalized into generic `DataTable` CRUD view.            |
| `src/routes/favorites.tsx`                 | **GENERALIZE** | Saved record list generalized into reusable table/list filter views.                       |
| `src/routes/fuel.tsx`                      | **GENERALIZE** | Replaced with neutral multi-metric `ReportsView` with date range and CSV export.           |
| `src/routes/fx.tsx`                        | **GENERALIZE** | Rate table pattern absorbed into generic `DataTable`.                                      |
| `src/routes/livestock.tsx`                 | **GENERALIZE** | Commodity catalog view generalized into reusable card grid / CRUD records.                 |
| `src/routes/regions.tsx`                   | **GENERALIZE** | Regional breakdown generalized into standard analytics views.                              |
| `src/router.tsx`                           | **GENERALIZE** | Converted to React Router 7 `createBrowserRouter` in `src/app/router.tsx`.                 |
| `src/styles.css`                           | **GENERALIZE** | Modernized into `src/styles/globals.css` with centralized design tokens.                   |
| `src/components/ui/accordion.tsx`          | **KEEP**       | Generic Radix UI primitive.                                                                |
| `src/components/ui/alert-dialog.tsx`       | **KEEP**       | Generic Radix UI primitive.                                                                |
| `src/components/ui/alert.tsx`              | **KEEP**       | Generic alert UI primitive.                                                                |
| `src/components/ui/aspect-ratio.tsx`       | **KEEP**       | Generic aspect ratio container primitive.                                                  |
| `src/components/ui/avatar.tsx`             | **KEEP**       | Generic avatar primitive.                                                                  |
| `src/components/ui/badge.tsx`              | **KEEP**       | Generic badge primitive with style variants.                                               |
| `src/components/ui/breadcrumb.tsx`         | **KEEP**       | Generic accessible breadcrumb navigation primitive.                                        |
| `src/components/ui/button.tsx`             | **KEEP**       | Generic button primitive with CVA variants.                                                |
| `src/components/ui/calendar.tsx`           | **KEEP**       | Generic React Day Picker calendar component.                                               |
| `src/components/ui/card.tsx`               | **KEEP**       | Generic card primitive.                                                                    |
| `src/components/ui/carousel.tsx`           | **KEEP**       | Generic Embla carousel wrapper.                                                            |
| `src/components/ui/chart.tsx`              | **KEEP**       | Generic Recharts responsive container and tooltip integration.                             |
| `src/components/ui/checkbox.tsx`           | **KEEP**       | Generic Radix checkbox primitive.                                                          |
| `src/components/ui/collapsible.tsx`        | **KEEP**       | Generic collapsible container primitive.                                                   |
| `src/components/ui/command.tsx`            | **KEEP**       | Generic CmdK command palette primitive.                                                    |
| `src/components/ui/context-menu.tsx`       | **KEEP**       | Generic context menu primitive.                                                            |
| `src/components/ui/dialog.tsx`             | **KEEP**       | Generic modal dialog primitive.                                                            |
| `src/components/ui/drawer.tsx`             | **KEEP**       | Generic Vaul drawer primitive.                                                             |
| `src/components/ui/dropdown-menu.tsx`      | **KEEP**       | Generic dropdown menu primitive.                                                           |
| `src/components/ui/form.tsx`               | **KEEP**       | Generic React Hook Form integration wrappers.                                              |
| `src/components/ui/hover-card.tsx`         | **KEEP**       | Generic hover card primitive.                                                              |
| `src/components/ui/input-otp.tsx`          | **KEEP**       | Generic one-time password input primitive.                                                 |
| `src/components/ui/input.tsx`              | **KEEP**       | Generic input primitive.                                                                   |
| `src/components/ui/label.tsx`              | **KEEP**       | Generic label primitive.                                                                   |
| `src/components/ui/menubar.tsx`            | **KEEP**       | Generic menubar primitive.                                                                 |
| `src/components/ui/navigation-menu.tsx`    | **KEEP**       | Generic navigation menu primitive.                                                         |
| `src/components/ui/pagination.tsx`         | **KEEP**       | Generic pagination control primitive.                                                      |
| `src/components/ui/popover.tsx`            | **KEEP**       | Generic popover primitive.                                                                 |
| `src/components/ui/progress.tsx`           | **KEEP**       | Generic progress bar primitive.                                                            |
| `src/components/ui/radio-group.tsx`        | **KEEP**       | Generic radio group primitive.                                                             |
| `src/components/ui/resizable.tsx`          | **KEEP**       | Generic resizable panel group primitive.                                                   |
| `src/components/ui/scroll-area.tsx`        | **KEEP**       | Generic scroll area primitive.                                                             |
| `src/components/ui/select.tsx`             | **KEEP**       | Generic select dropdown primitive.                                                         |
| `src/components/ui/separator.tsx`          | **KEEP**       | Generic separator primitive.                                                               |
| `src/components/ui/sheet.tsx`              | **KEEP**       | Generic slide-over sheet primitive.                                                        |
| `src/components/ui/sidebar.tsx`            | **KEEP**       | Generic collapsible sidebar primitive.                                                     |
| `src/components/ui/skeleton.tsx`           | **KEEP**       | Generic skeleton placeholder primitive.                                                    |
| `src/components/ui/slider.tsx`             | **KEEP**       | Generic slider primitive.                                                                  |
| `src/components/ui/sonner.tsx`             | **KEEP**       | Generic Sonner toaster primitive.                                                          |
| `src/components/ui/switch.tsx`             | **KEEP**       | Generic switch toggle primitive.                                                           |
| `src/components/ui/table.tsx`              | **KEEP**       | Generic table markup primitives.                                                           |
| `src/components/ui/tabs.tsx`               | **KEEP**       | Generic tabs primitive.                                                                    |
| `src/components/ui/textarea.tsx`           | **KEEP**       | Generic textarea primitive.                                                                |
| `src/components/ui/toggle-group.tsx`       | **KEEP**       | Generic toggle group primitive.                                                            |
| `src/components/ui/toggle.tsx`             | **KEEP**       | Generic toggle button primitive.                                                           |
| `src/components/ui/tooltip.tsx`            | **KEEP**       | Generic tooltip primitive.                                                                 |
| `src/hooks/use-mobile.tsx`                 | **KEEP**       | Generic responsive mobile breakpoint hook.                                                 |
| `src/lib/utils.ts`                         | **KEEP**       | Generic `cn()` clsx + twMerge utility.                                                     |

---

## State and Data Flow

- **Authentication**: `src/features/auth/auth.tsx` provides `AuthProvider`, `useAuth()`, and `ProtectedRoute` using browser `localStorage` and `tokenStorage`. No backend is simulated or implied.
- **Server State / API Layer**: `src/lib/api.ts` provides a lightweight `fetch` wrapper supporting base URL configuration (`VITE_API_BASE_URL`), request/response interceptors, and typed `ApiError`. `src/lib/example-api.ts` illustrates a TanStack Query hook pattern (`useQuery`, `useMutation`).
- **Client Localization**: `src/features/i18n/i18n.tsx` provides an `I18nProvider` supporting English (`en`) and Swahili (`sw`) with a safe default fallback.
- **Client Theme**: `src/app/ThemeProvider.tsx` persists light/dark/system preferences in `localStorage` and syncs with `prefers-color-scheme`.
- **CRUD State**: `src/features/example-crud/RecordsView.tsx` demonstrates full in-memory CRUD operations with search, column visibility toggles, sorting, pagination, and CSV export.

---

## Dead Code & Removals

- Removed unused sample script `public/sample/Bei.jsx`.
- Removed 12 static commodity image files in `src/assets/`.
- Removed 15 static agricultural and oil datasets in `src/data/`.
- Removed unused legacy config files (`wrangler.jsonc`, `netlify.toml`).
- Replaced TanStack Start router generation (`routeTree.gen.ts`) with a clean single configuration array in `src/app/routes.tsx`.
