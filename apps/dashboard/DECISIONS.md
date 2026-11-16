# Engineering Decisions Log

Every architectural and implementation judgment call made during the template conversion is recorded here with its rationale.

---

### 1. Repository History & Branch Strategy

- **Decision**: Maintained conversion work on the dedicated branch `template-conversion`.
- **Reason**: Allows the downstream developer to review the full step-by-step diff and preserve clean commit history.

### 2. Domain Generalization vs. POS Assumptions

- **Decision**: Recognized that the source application was a Tanzanian commodity price tracking UI (crops, livestock, fuel, foreign exchange) rather than a Point-of-Sale (POS) system. Generalized it into a modern, multi-purpose admin and application template rather than trying to force-fit POS logic.
- **Reason**: The components (stat cards, trend indicators, Recharts wrappers, data tables, filter dialogs) naturally form a robust, domain-neutral dashboard template that fits SaaS, internal tools, ERPs, CRM, or data portals.

### 3. Target Directory Organization

- **Decision**: Refactored `src/` to strictly follow the target structure:
  - `src/app/`: `App.tsx`, `providers.tsx`, `router.tsx`, `routes.tsx`, `ThemeProvider.tsx`.
  - `src/features/`: `auth/`, `dashboard/`, `settings/`, `reports/`, `example-crud/`, `i18n/`.
  - `src/components/`: `ui/` (Radix primitives), `shared/` (composed cards, charts, data tables), `layout/` (app shell, auth layout).
  - `src/pages/`: Route targets that expose and compose the feature components.
  - `src/types/`: Centralized domain-neutral TypeScript interfaces.
  - `src/styles/`: Centralized design tokens in `src/styles/globals.css`.
  - `src/assets/`: Neutral SVG logos and placeholders.
- **Reason**: Provides clean separation of concerns: features are modular and reusable, UI primitives are unopinionated, and routing is driven by a single config array.

### 4. Routing Architecture

- **Decision**: Replaced TanStack Start file-based routing with React Router 7 (`createBrowserRouter`) driven by a single routes array in `src/app/routes.tsx`.
- **Reason**: Allows downstream developers to add a new page by creating a page file and appending a single configuration entry.

### 5. Authentication Scaffold

- **Decision**: Built a client-side `AuthProvider` and `ProtectedRoute` supporting role-based access (`member`, `admin`) and local session persistence.
- **Reason**: Avoids coupling to an external backend service while providing a functional scaffold that developers can connect to Auth0, Supabase, Firebase, or custom OAuth backends.

### 6. Optional i18n (English + Swahili)

- **Decision**: Provided a lightweight, type-safe i18n context with English and Swahili translations, complete with a toolbar and settings language switcher. Designed `useI18n()` to safely fallback to English if used outside `I18nProvider`.
- **Reason**: Directly satisfies the optional Phase 3 i18n requirement while honoring the original Tanzanian linguistic roots of the source project. Fallback behavior prevents isolated component or unit test crashes.

### 7. Form Validation with React Hook Form + Zod

- **Decision**: Standardized form management on React Hook Form + Zod across both the example CRUD forms and authentication screens.
- **Reason**: Industry-standard, type-safe schema validation that handles error messages, accessibility labels, and clean submission lifecycles.

### 8. State Management Strategy

- **Decision**: Utilized TanStack Query v5 for asynchronous server state and React Context for lightweight client UI state (theme, auth, i18n).
- **Reason**: Eliminates unnecessary Redux or external state-library boilerplate for common web applications.

### 9. Linting and Fast Refresh Handling

- **Decision**: Configured ESLint flat config (`eslint.config.js`) to allow specific intentional co-exports (`buttonVariants`, `useAuth`, `useI18n`) for component library ergonomics.
- **Reason**: Keeps developer ergonomics high while ensuring strict TypeScript and Prettier adherence across the codebase.

### 10. Residual Terminology Audit

- **Decision**: Verified zero business-domain terms remain in code or HTML. The word `region` appears strictly as the standard W3C ARIA landmark role (`role="region"`), and historical terms appear only in `ANALYSIS.md`, `DECISIONS.md`, and `MIGRATION_NOTES.md`.
- **Reason**: Standard accessibility attributes must not be scrubbed, and migration logs must record the actual historical context.

### 11. Licensing Notice

- **Decision**: Documented that the original snapshot contained no LICENSE file.
- **Reason**: Ensures downstream users are informed that intellectual property clearances should be verified prior to commercial use.
