# Frontend Application Template

A modern, production-ready, domain-neutral frontend template built with **React 18 + Vite + TypeScript**. Designed to be cloned, customized with your own pages and APIs, and shipped in minutes.

---

## ⚡ Tech Stack

- **Core**: React 18.3, TypeScript 5.8 (Strict Mode), Vite 5.4 (SWC Fast Compiler)
- **Routing**: React Router 6.30 Data Router (`createBrowserRouter`), code-split lazy routes, custom ErrorBoundary
- **State & Server Cache**: TanStack React Query v5 (queries & mutations), React Context (Auth, Theme, Language)
- **Styling & Design System**: Tailwind CSS 3.4, Radix UI Primitives, Lucide Icons, HSL CSS design tokens (light/dark/system mode)
- **Forms & Validation**: React Hook Form v7, Zod v3, `@hookform/resolvers`
- **Data Visualization**: Recharts v2 (Area, Bar, Line, Pie charts)
- **Feedback & Alerts**: Sonner toasts, accessible confirmation dialogs, cmdk command palette
- **Testing**: Vitest v2, React Testing Library, Jest DOM matchers, JSDOM
- **CI / Deployment**: Multi-stage Dockerfile (Nginx Alpine), GitHub Actions CI workflow, Netlify SPA routing

---

## 🚀 Quick Start

### Prerequisites
- Node.js 18+ (Node 20 or 22 recommended)
- npm 9+

### 1. Install Dependencies
```bash
npm install
```

### 2. Configure Environment
```bash
cp .env.example .env
```

### 3. Start Development Server
```bash
npm run dev
```
The application will launch at `http://localhost:3000` (or `http://localhost:8093`).

### 4. Run Verification Suite
```bash
npm run typecheck    # TypeScript strict type checking
npm run lint         # ESLint checking
npm run test         # Unit & component test runner (Vitest)
npm run build        # Production build with chunking
```

---

## 📁 Folder Structure

```
src/
├── app/                  # Application initialization & routing
│   ├── App.tsx           # Root provider wrapper
│   ├── providers.tsx     # TanStack Query, Theme, Language, Auth providers
│   ├── router.tsx        # createBrowserRouter data router
│   └── routes.config.tsx # UNIFIED route & navigation config array
├── assets/               # Static images and icons
├── components/
│   ├── layout/           # AppShell, Collapsible Sidebar, Topbar, Breadcrumbs, Footer
│   ├── shared/           # DataTable, StatCard, ChartCard, ActivityFeed, ConfirmDialog, etc.
│   └── ui/               # Radix & shadcn UI primitives (Button, Input, Modal, Select, etc.)
├── contexts/             # Client state contexts (AuthContext, ThemeContext, LanguageContext)
├── features/             # Feature slices (CRUD example, auth, dashboard, reports, settings)
│   └── example-crud/     # CrudForm, TanStack Query hooks (crudApi.ts)
├── hooks/                # Custom hooks (useDebounce, useLocalStorage, usePagination, etc.)
├── lib/                  # Utilities (cn, formatters), API client (Axios), translations, constants
├── pages/                # Screen components (Dashboard, List, Detail, Reports, Settings, Auth)
├── test/                 # Test suites (DataTable, CrudForm, Modal, ProtectedRoute)
├── types/                # TypeScript interfaces (auth, api, crud, navigation, dashboard)
└── index.css             # Tailwind directives & HSL CSS design tokens
```

---

## 🛠️ How-To Guides

### How to Add a New Page (2 Steps)

#### Step 1: Create your page component
Create a file at `src/pages/MyNewPage.tsx`:
```tsx
import * as React from "react";
import { StatCard } from "@/components/shared/StatCard";

export function MyNewPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">My New Feature</h1>
      <StatCard title="Active Items" value="256" change={5.2} />
    </div>
  );
}

export default MyNewPage;
```

#### Step 2: Register in `src/app/routes.config.tsx`
Add one entry to the `routesConfig` array:
```tsx
import { Sparkles } from "lucide-react";
const MyNewPage = lazy(() => import("@/pages/MyNewPage"));

// In routesConfig array:
{
  path: "/my-feature",
  title: "My Feature",
  icon: Sparkles,
  element: <MyNewPage />,
  layout: "dashboard",
  inNav: true,
  navSection: "main",
},
```
*That's it!* The collapsible sidebar, mobile drawer, breadcrumbs, and router will automatically detect and render your new page.

---

### How to Make an API Call

Use the configured `apiClient` (`src/lib/api.ts`) with TanStack React Query:

```tsx
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";

// 1. Fetching data (Query)
export function useItemsQuery() {
  return useQuery({
    queryKey: ["items"],
    queryFn: async () => {
      const response = await apiClient.get("/items");
      return response.data;
    },
  });
}

// 2. Creating data (Mutation)
export function useCreateItemMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (newItem: { name: string }) => {
      const response = await apiClient.post("/items", newItem);
      return response.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["items"] });
    },
  });
}
```

---

### How to Create or Customize a Reusable Component

1. Add primitive UI components to `src/components/ui/` (using Radix UI and Tailwind CSS).
2. Add multi-component composite widgets to `src/components/shared/` and re-export in `src/components/shared/index.ts`.
3. Use the `cn()` helper from `@/lib/utils` for conditional class merging.

---

### How to Change the Theme & Design Tokens

All theme tokens (primary colors, background, borders, radius) are defined as HSL variables in `src/index.css`. To re-theme the entire application:

1. Open `src/index.css`.
2. Update the `:root` (light) and `.dark` (dark) HSL values:
```css
:root {
  /* Change primary color to Indigo: */
  --primary: 240 5.9% 10%;
  --primary-foreground: 0 0% 98%;

  /* Change border radius: */
  --radius: 0.75rem;
}
```
The entire UI (buttons, active navigation items, charts, badges, focus rings) will automatically inherit the new theme.

---

## 🔒 Authentication & Role-Based Access Control

The template includes an out-of-the-box authentication scaffold (`src/contexts/AuthContext.tsx`):
- **Preconfigured Demo Users**: Admin, Manager, and Standard User.
- **Role Switcher**: Click the user menu in the top right to instantly toggle between roles and test role-based view permissions.
- **Route Guards**: Protect any route or component by specifying `allowedRoles={['admin']}` in `ProtectedRoute`.

---

## 🌐 Localization (i18n)

Built-in multi-language context (`src/contexts/LanguageContext.tsx`) with English and Swahili (Kiswahili) dictionaries:
- Switch language on-the-fly using the topbar language pill (`EN` / `SW`).
- Add translation strings to `src/lib/translations.ts`.
- Access translations in any component with `const { t } = useLanguage();`.

---

## 🐳 Docker Deployment

Build and run using the optimized multi-stage Dockerfile:

```bash
# Build the Docker image
docker build -t frontend-template .

# Run the container
docker run -p 8080:80 frontend-template
```
Open `http://localhost:8080` in your browser.

---

## 📄 License
Check `MIGRATION_NOTES.md` for license and migration history details. Ensure you apply your appropriate organization license (e.g. MIT, Apache 2.0, or proprietary) before distribution.
