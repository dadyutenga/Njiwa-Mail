# Component Catalog & API Reference

This catalog documents every reusable UI primitive, data presentation component, feedback dialog, and custom hook available in the template. Each entry contains property definitions and quick usage examples for rapid development.

---

## 1. UI Primitives (`src/components/ui/`)

### `Button`
Standard polymorphic button supporting visual variants and loading/disabled states.
- **Props**: `variant` ("default" | "destructive" | "outline" | "secondary" | "ghost" | "link"), `size` ("default" | "sm" | "lg" | "icon"), `asChild` (boolean).
```tsx
<Button variant="default" size="sm" onClick={() => save()}>Save Changes</Button>
```

### `IconButton`
Accessible icon-only button requiring an explicit accessible label.
- **Props**: `aria-label` (string, required), `size` ("default" | "sm" | "lg" | "icon"), `variant` (Button variants).
```tsx
<IconButton aria-label="Delete entry" variant="ghost" onClick={handleDelete}><Trash className="h-4 w-4" /></IconButton>
```

### `Input`
Styled text input with full accessibility and focus ring styling.
- **Props**: Standard HTML input attributes (`type`, `placeholder`, `disabled`, `value`, `onChange`).
```tsx
<Input type="text" placeholder="Search keywords..." value={query} onChange={(e) => setQuery(e.target.value)} />
```

### `PasswordInput`
Secure password field with an integrated toggle button to show or hide plain text.
- **Props**: Standard HTML input attributes.
```tsx
<PasswordInput placeholder="Enter your password" {...field} />
```

### `Select`
Radix-based accessible dropdown select menu.
- **Props**: `value` (string), `onValueChange` ((val: string) => void), `defaultValue` (string).
```tsx
<Select value={role} onValueChange={setRole}><SelectTrigger><SelectValue placeholder="Select role" /></SelectTrigger><SelectContent><SelectItem value="admin">Admin</SelectItem></SelectContent></Select>
```

### `MultiSelect`
Multi-selection dropdown with chip badges, search filter, and individual item removal.
- **Props**: `options` ({ label: string; value: string }[]), `selected` (string[]), `onChange` ((val: string[]) => void), `placeholder` (string).
```tsx
<MultiSelect options={[{ label: "Tag A", value: "a" }]} selected={tags} onChange={setTags} placeholder="Select tags..." />
```

### `Textarea`
Multi-line text area supporting form validation and flexible sizing.
- **Props**: Standard HTML textarea attributes.
```tsx
<Textarea placeholder="Enter description..." rows={4} {...field} />
```

### `Checkbox`
Radix checkbox with custom animated checkmark.
- **Props**: `checked` (boolean), `onCheckedChange` ((checked: boolean) => void), `disabled` (boolean).
```tsx
<Checkbox checked={agreed} onCheckedChange={(val) => setAgreed(!!val)} id="terms" />
```

### `RadioGroup` & `RadioGroupItem`
Accessible mutually exclusive radio group.
- **Props**: `value` (string), `onValueChange` ((val: string) => void).
```tsx
<RadioGroup value={plan} onValueChange={setPlan}><RadioGroupItem value="pro" id="pro" /><Label htmlFor="pro">Pro</Label></RadioGroup>
```

### `Switch`
Accessible boolean toggle switch with smooth transition animation.
- **Props**: `checked` (boolean), `onCheckedChange` ((val: boolean) => void).
```tsx
<Switch checked={notificationsEnabled} onCheckedChange={setNotificationsEnabled} />
```

### `DatePicker`
Single-date picker displaying formatted date with popover calendar.
- **Props**: `date` (Date), `onSelect` ((date: Date | undefined) => void), `placeholder` (string).
```tsx
<DatePicker date={selectedDate} onSelect={setSelectedDate} placeholder="Select scheduled date" />
```

### `DateRangePicker`
Date range selector with quick preset shortcuts (Last 7d, 30d, 90d) and dual-month calendar.
- **Props**: `dateRange` ({ from?: Date; to?: Date }), `onSelect` ((range: DateRange | undefined) => void), `showPresets` (boolean).
```tsx
<DateRangePicker dateRange={range} onSelect={setRange} showPresets={true} />
```

### `FileUpload`
Drag-and-drop file upload zone with file size formatting, previews, max limit check, and file removal.
- **Props**: `onFilesSelected` ((files: File[]) => void), `maxFiles` (number), `maxSizeMB` (number), `accept` (string).
```tsx
<FileUpload onFilesSelected={(files) => handleUpload(files)} maxFiles={5} maxSizeMB={10} accept="image/*,.pdf" />
```

### `Badge`
Compact label for statuses, categories, or counts.
- **Props**: `variant` ("default" | "secondary" | "destructive" | "outline"), `children` (ReactNode).
```tsx
<Badge variant="outline" className="capitalize">Active</Badge>
```

### `Avatar`
User profile avatar with image source and automatic initials fallback.
- **Props**: `src` (string), `alt` (string), `fallback` (string).
```tsx
<Avatar><AvatarImage src={user.avatarUrl} /><AvatarFallback>{user.initials}</AvatarFallback></Avatar>
```

### `Tooltip`
Accessible hover tooltip with customizable delay and positioning.
- **Props**: `children` (ReactNode), `content` (ReactNode), `side` ("top" | "right" | "bottom" | "left").
```tsx
<Tooltip><TooltipTrigger asChild><Button size="icon"><Info /></Button></TooltipTrigger><TooltipContent>More info</TooltipContent></Tooltip>
```

### `Tabs`, `TabsList`, `TabsTrigger`, `TabsContent`
Tabbed content manager for multi-section screens.
- **Props**: `defaultValue` (string), `value` (string), `onValueChange` ((val: string) => void).
```tsx
<Tabs defaultValue="account"><TabsList><TabsTrigger value="account">Account</TabsTrigger></TabsList><TabsContent value="account">Content</TabsContent></Tabs>
```

### `Accordion`
Collapsible multi-section disclosure container.
- **Props**: `type` ("single" | "multiple"), `collapsible` (boolean).
```tsx
<Accordion type="single" collapsible><AccordionItem value="1"><AccordionTrigger>Q1?</AccordionTrigger><AccordionContent>A1.</AccordionContent></AccordionItem></Accordion>
```

### `Spinner`
Accessible SVG loading spinner.
- **Props**: `size` ("sm" | "md" | "lg"), `className` (string).
```tsx
<Spinner size="md" />
```

### `Progress`
Horizontal percentage bar.
- **Props**: `value` (number: 0 - 100), `className` (string).
```tsx
<Progress value={65} className="h-2" />
```

### `EmptyState`
Placeholder view for empty tables, search results, or data collections.
- **Props**: `icon` (LucideIcon), `title` (string), `description` (string), `actionLabel` (string), `onAction` (() => void).
```tsx
<EmptyState title="No records found" description="Try adjusting your filter" actionLabel="Create Record" onAction={openModal} />
```

### `Alert`
Highlighted callout banner for notifications, errors, warnings, or tips.
- **Props**: `variant` ("default" | "destructive"), `title` (string), `description` (string).
```tsx
<Alert variant="destructive"><AlertTitle>Error</AlertTitle><AlertDescription>Unable to reach server.</AlertDescription></Alert>
```

### `Stepper`
Step progress tracker for multi-step wizards or checkout flows.
- **Props**: `steps` ({ id: string | number; title: string; description?: string }[]), `currentStep` (number), `onStepClick` ((step: number) => void).
```tsx
<Stepper steps={[{ id: 1, title: "Account" }, { id: 2, title: "Review" }]} currentStep={currentStep} />
```

---

## 2. Data & Analytics (`src/components/shared/`)

### `DataTable`
Feature-complete data table with sorting, filtering, full-text search, multi-row selection, column visibility, row actions menu, and CSV export.
- **Props**:
  - `data`: `T[]` (items array)
  - `columns`: `ColumnDef<T>[]` (column definitions)
  - `isLoading`?: boolean
  - `searchPlaceholder`?: string
  - `filterOptions`?: FilterOption[]
  - `onRowClick`?: (item: T) => void
  - `onEdit`?: (item: T) => void
  - `onDelete`?: (item: T) => void
  - `exportFileName`?: string
  - `pageSize`?: number
```tsx
<DataTable data={records} columns={columns} onEdit={handleEdit} onDelete={handleDelete} exportFileName="records.csv" />
```

### `StatCard`
Executive KPI summary card displaying metric value, percentage trend change, and icon.
- **Props**: `title` (string), `value` (string | number), `change` (number), `period` (string), `icon` (LucideIcon), `description` (string).
```tsx
<StatCard title="Total Volume" value="$124,500" change={14.2} period="vs. last month" icon={DollarSign} />
```

### `ChartCard`
Card wrapper for charts with title, subtitle, header action slots, and loading skeletons.
- **Props**: `title` (string), `description` (string), `action` (ReactNode), `isLoading` (boolean), `children` (ReactNode).
```tsx
<ChartCard title="Revenue Growth" description="Monthly comparison" action={<Button size="sm">Export</Button>}><AreaChartWrapper data={data} categories={cats} /></ChartCard>
```

### `AreaChartWrapper`, `BarChartWrapper`, `LineChartWrapper`, `PieChartWrapper`
Pre-styled, responsive Recharts wrappers adhering to the template's color palette and dark mode tokens.
```tsx
<AreaChartWrapper data={revenueData} categories={[{ key: "rev", name: "Revenue", color: "hsl(var(--primary))" }]} height={300} />
<BarChartWrapper data={categoryData} bars={[{ key: "count", name: "Records" }]} height={280} />
<PieChartWrapper data={statusDistribution} innerRadius={60} outerRadius={90} height={260} />
```

### `ActivityFeed`
Chronological activity timeline featuring user avatar, action badge, and relative timestamps.
- **Props**: `items` (ActivityItem[]), `emptyMessage` (string).
```tsx
<ActivityFeed items={activityItems} />
```

### `KpiTrend`
Badge displaying positive/negative/neutral percentage indicators with up/down arrows.
- **Props**: `value` (number), `suffix` (string, default: "%"), `showIcon` (boolean).
```tsx
<KpiTrend value={12.8} />
```

---

## 3. Feedback & Navigation (`src/components/shared/`)

### `ConfirmDialog`
Modal dialog for verifying destructive actions (e.g. deletion, cancellation).
- **Props**: `open` (boolean), `onOpenChange` ((open: boolean) => void), `title` (string), `description` (string), `confirmLabel` (string), `variant` ("default" | "destructive"), `onConfirm` (() => void), `isLoading` (boolean).
```tsx
<ConfirmDialog open={isOpen} onOpenChange={setIsOpen} title="Delete Record?" description="This cannot be undone." variant="destructive" onConfirm={handleDelete} />
```

### `Modal`
Declarative wrapper around Radix Dialog for forms, wizards, and previews.
- **Props**: `open` (boolean), `onOpenChange` ((open: boolean) => void), `title` (string), `description` (string), `footer` (ReactNode), `children` (ReactNode).
```tsx
<Modal open={isOpen} onOpenChange={setIsOpen} title="Edit Record"><CrudForm onSubmit={save} /></Modal>
```

### `ErrorBoundary`
Graceful error boundary component that catches runtime rendering failures and provides reset/reload buttons.
```tsx
<ErrorBoundary><DashboardContent /></ErrorBoundary>
```

### `PageLoader`
Full-height loading spinner with optional customizable status message.
- **Props**: `message` (string, default: "Loading...").
```tsx
<PageLoader message="Fetching account records..." />
```

### `CommandPalette`
Global `Cmd+K` / `Ctrl+K` searchable modal command palette for rapid navigation and actions.
```tsx
<CommandPalette open={open} onOpenChange={setOpen} />
```

### `ThemeToggle`
Dropdown button for switching between Light, Dark, and System theme modes.
```tsx
<ThemeToggle />
```

### `UserMenu`
Topbar profile menu featuring user details, role badge, demo role-switching buttons, settings link, and sign-out action.
```tsx
<UserMenu />
```

### `NotificationBell`
Topbar bell icon with live unread indicator and popover notifications dropdown.
```tsx
<NotificationBell />
```

### `PrintableDocument`
Clean printable document template formatted with `@media print` rules for invoices, summaries, or reports.
- **Props**: `title` (string), `documentNumber` (string), `date` (string), `author` (string), `status` (string), `children` (ReactNode).
```tsx
<PrintableDocument title="Audit Summary" documentNumber="DOC-102">{/* details */}</PrintableDocument>
```

### `CopyButton`
One-click clipboard copy button with visual checkmark feedback.
- **Props**: `value` (string), `label` (string).
```tsx
<CopyButton value="REC-1002" label="Copy Code" />
```

---

## 4. Custom Hooks (`src/hooks/`)

| Hook | Return Type | Description & Usage Example |
| :--- | :--- | :--- |
| `useDebounce<T>(value, delay)` | `T` | Debounces rapidly changing values (e.g. search input). `const debounced = useDebounce(query, 300);` |
| `useLocalStorage<T>(key, initial)` | `[T, (val) => void]` | Persists state in `localStorage` with JSON serialization. `const [pref, setPref] = useLocalStorage("pref", true);` |
| `useMediaQuery(query)` | `boolean` | Listens to CSS media query changes. `const isDesktop = useMediaQuery("(min-width: 1024px)");` |
| `useIsMobile()` | `boolean` | Returns `true` when viewport is smaller than 768px. `const isMobile = useIsMobile();` |
| `useClickOutside(ref, handler)` | `void` | Triggers a callback when clicking outside an element ref. `useClickOutside(menuRef, () => close());` |
| `usePagination(options)` | `UsePaginationResult` | Manages page state, total calculation, next/prev helpers. `const { page, totalPages, nextPage } = usePagination({ totalItems: 100 });` |
| `useDisclosure(initialState)` | `UseDisclosureResult` | Boolean modal/drawer disclosure state helper. `const { isOpen, open, close, toggle } = useDisclosure();` |
| `useToast()` / `toast` | `Toast` | Dispatches toast alerts via sonner or Radix. `toast.success("Saved successfully");` |
