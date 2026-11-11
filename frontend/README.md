# Frontend Template

A production-ready, domain-neutral React + Vite + TypeScript application template with Tailwind CSS v4, Radix UI primitives, React Router 7, TanStack Query, React Hook Form, Zod, and Recharts.

Designed to be cloned, customized, and shipped in minutes.

---

## Tech Stack

- **Runtime & Language**: React 19, TypeScript (strict mode).
- **Build System**: Vite 8 with `@vitejs/plugin-react`.
- **Routing**: React Router 7 (`react-router-dom`) with `createBrowserRouter`, lazy route splitting, and error boundaries.
- **Styling**: Tailwind CSS v4 with centralized OKLCH design tokens.
- **UI Components**: Radix UI primitives, Lucide React icons, Vaul drawer, Sonner toasts.
- **Server State**: TanStack Query v5 (`@tanstack/react-query`).
- **Form Management**: React Hook Form with Zod schema validation.
- **Data Visualization**: Recharts 2 with responsive wrappers.
- **Localization**: Built-in optional bilingual support (English & Swahili) with live language switching.
- **Testing**: Vitest + React Testing Library + jsdom.
- **Quality**: ESLint 9 (flat config) + Prettier.
- **Deployment**: Multi-stage `Dockerfile` (Node 24 build & Nginx alpine) and GitHub Actions CI.

---

## Quickstart & How to Run

Requirements: **Node.js >= 24** and **npm**.

```bash
# 1. Install dependencies
npm install

# 2. Start local development server
npm run dev

# 3. Run test suite
npm test

# 4. Check linting and formatting
npm run lint

# 5. Build production bundle
npm run build
```

Once running, visit `http://localhost:5173`. Any email address and a password of at least 8 characters will log you into the local demo session.

---

## Folder Structure

```
src/
├── app/                  # Application bootstrap & shell config
│   ├── App.tsx           # Top-level root component
│   ├── providers.tsx     # Consolidated context providers
│   ├── router.tsx        # React Router createBrowserRouter config
│   ├── routes.tsx        # Single navigation & route declaration array
│   └── ThemeProvider.tsx # Dark/Light/System theme provider
├── assets/               # Brand & SVG vector assets
├── components/           # Reusable UI component library
│   ├── layout/           # AppShell, DashboardLayout, AuthLayout
│   ├── shared/           # Composed components (DataTable, Charts, Display cards)
│   └── ui/               # Radix primitives (Button, Dialog, Input, Select, etc.)
├── features/             # Modular feature slices
│   ├── auth/             # AuthProvider, ProtectedRoute, AuthForm
│   ├── dashboard/        # Dashboard view, stat cards, activity feed
│   ├── example-crud/     # Records list, modal create/edit dialog, mock store
│   ├── i18n/             # English/Swahili translation provider & switcher
│   ├── reports/          # Analytics metrics, date-range picker, CSV export
│   └── settings/         # Account, appearance, and notification settings
├── hooks/                # Reusable custom React hooks
├── lib/                  # Helpers, API client, formatters, validators
├── pages/                # Lazy-loaded route target components
├── styles/               # Centralized Tailwind CSS design tokens & global CSS
└── types/                # Domain-neutral TypeScript interfaces
```

---

## How to Add a Page

Adding a new page requires only **two simple steps**:

### Step 1: Create the Page Component

Create your page component in `src/pages/Analytics.tsx`:

```tsx
// src/pages/Analytics.tsx
export default function AnalyticsPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold tracking-tight">Analytics</h1>
      <p className="text-muted-foreground">My new custom dashboard view.</p>
    </div>
  );
}
```

### Step 2: Register in `src/app/routes.tsx`

Add a lazy import and one entry to the `routes` array:

```tsx
// src/app/routes.tsx
import { LineChart } from "lucide-react";
const AnalyticsPage = lazy(() => import("@/pages/Analytics"));

export const routes = [
  // ... existing routes
  {
    path: "/analytics",
    label: "Analytics",
    icon: LineChart,
    component: AnalyticsPage,
    navigation: true, // set to false for parameterized routes like /records/:id
  },
];
```

The route is now automatically mounted in React Router and added to the sidebar navigation and search command palette (`Ctrl+K`).

---

## How to Add an API Call

The template includes an interceptor-ready HTTP client in `src/lib/api.ts` and TanStack Query integration.

### Example: Define a Query and Mutation Hook

```tsx
// src/features/items/api.ts
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiRequest } from "@/lib/api";

interface Item {
  id: string;
  name: string;
}

// GET /items
export function useItems() {
  return useQuery({
    queryKey: ["items"],
    queryFn: () => apiRequest<Item[]>("/items"),
  });
}

// POST /items
export function useCreateItem() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (newItem: Omit<Item, "id">) =>
      apiRequest<Item>("/items", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(newItem),
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["items"] });
    },
  });
}
```

---

## How to Add a Component

1. Place generic primitives in `src/components/ui/` (e.g. `src/components/ui/badge.tsx`).
2. Place composed or multi-element widgets in `src/components/shared/` (e.g. `src/components/shared/MyCard.tsx`).
3. Import using the `@/` path alias:

```tsx
import { Button } from "@/components/ui/button";
import { StatCard } from "@/components/shared/Display";
```

All reusable components are indexed with code snippets in [COMPONENT_CATALOG.md](COMPONENT_CATALOG.md).

---

## How to Change the Theme

Design tokens are centralized in `src/styles/globals.css`.

- **Colors & Radii**: Adjust the CSS variables under `:root` (light mode) and `:root.dark` (dark mode).
- **Primary Color**: Modify `--primary` and `--primary-foreground`.
- **Border Radius**: Change `--radius` (defaults to `0.875rem`).
- **Fonts**: Update `--font-sans` or `--font-display`.

```css
/* src/styles/globals.css */
:root {
  --primary: oklch(0.52 0.14 155); /* Custom brand color */
  --radius: 0.75rem;
}
```

The app shell includes a theme toggle and persists preference (`light`, `dark`, `system`) to `localStorage`.

---

## Environment Variables

Copy `.env.example` to `.env.local`:

```bash
cp .env.example .env.local
```

| Variable            | Description                                | Default                |
| :------------------ | :----------------------------------------- | :--------------------- |
| `VITE_API_BASE_URL` | Base URL for remote API endpoints          | `""` (mock/local mode) |
| `VITE_APP_TITLE`    | Brand title displayed in document & header | `"Your App"`           |

> [!CAUTION]
> Vite environment variables with the `VITE_` prefix are bundled into client code. **Never store secret keys, private tokens, or passwords in them.**

---

## Container & CI Deployment

### Multi-stage Docker

Build and run with Docker:

```bash
docker build -t frontend-template .
docker run -p 8080:80 frontend-template
```

### GitHub Actions

The automated CI workflow is configured in `.github/workflows/ci.yml`. On every push and pull request, it runs:

1. `npm run lint`
2. `npm run build`
3. `npm test`
