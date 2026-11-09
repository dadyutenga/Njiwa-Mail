import * as React from "react";
import {
  ChevronDown,
  ChevronUp,
  ChevronsUpDown,
  Download,
  Filter,
  MoreHorizontal,
  Search,
  SlidersHorizontal,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export interface ColumnDef<T> {
  key: keyof T | string;
  header: string;
  sortable?: boolean;
  render?: (item: T) => React.ReactNode;
}

export interface DataTableProps<T extends { id: string | number }> {
  data: T[];
  columns: ColumnDef<T>[];
  keyExtractor?: (item: T) => string | number;
  isLoading?: boolean;
  searchPlaceholder?: string;
  searchKey?: keyof T | string;
  filterOptions?: {
    key: keyof T;
    label: string;
    options: { label: string; value: string }[];
  }[];
  onRowClick?: (item: T) => void;
  onEdit?: (item: T) => void;
  onDelete?: (item: T) => void;
  onView?: (item: T) => void;
  customActions?: (item: T) => React.ReactNode;
  exportFileName?: string;
  pageSize?: number;
  className?: string;
  extraHeaderActions?: React.ReactNode;
}

export function DataTable<T extends { id: string | number }>({
  data,
  columns,
  keyExtractor = (item) => item.id,
  isLoading = false,
  searchPlaceholder = "Search records...",
  searchKey,
  filterOptions = [],
  onRowClick,
  onEdit,
  onDelete,
  onView,
  customActions,
  exportFileName = "export.csv",
  pageSize = 10,
  className,
  extraHeaderActions,
}: DataTableProps<T>) {
  const [search, setSearch] = React.useState("");
  const [sortKey, setSortKey] = React.useState<string | null>(null);
  const [sortOrder, setSortOrder] = React.useState<"asc" | "desc">("asc");
  const [page, setPage] = React.useState(1);
  const [selectedIds, setSelectedIds] = React.useState<Set<string | number>>(new Set());
  const [activeFilters, setActiveFilters] = React.useState<Record<string, string>>({});
  const [visibleColumns, setVisibleColumns] = React.useState<Set<string>>(() => {
    return new Set(columns.map((c) => String(c.key)));
  });

  // Filter & Search Logic
  const filteredData = React.useMemo(() => {
    return data.filter((item) => {
      // Global or key search
      if (search.trim()) {
        const query = search.toLowerCase();
        if (searchKey) {
          const val = String((item as Record<string, unknown>)[searchKey as string] ?? "");
          if (!val.toLowerCase().includes(query)) return false;
        } else {
          const match = Object.values(item).some((v) =>
            String(v).toLowerCase().includes(query),
          );
          if (!match) return false;
        }
      }

      // Column filters
      for (const [key, value] of Object.entries(activeFilters)) {
        if (value && value !== "all") {
          const itemVal = String((item as Record<string, unknown>)[key] ?? "");
          if (itemVal !== value) return false;
        }
      }

      return true;
    });
  }, [data, search, searchKey, activeFilters]);

  // Sort Logic
  const sortedData = React.useMemo(() => {
    if (!sortKey) return filteredData;
    return [...filteredData].sort((a, b) => {
      const aVal = (a as Record<string, unknown>)[sortKey];
      const bVal = (b as Record<string, unknown>)[sortKey];

      if (aVal === bVal) return 0;
      if (aVal === null || aVal === undefined) return 1;
      if (bVal === null || bVal === undefined) return -1;

      if (typeof aVal === "number" && typeof bVal === "number") {
        return sortOrder === "asc" ? aVal - bVal : bVal - aVal;
      }

      const aStr = String(aVal).toLowerCase();
      const bStr = String(bVal).toLowerCase();
      return sortOrder === "asc" ? aStr.localeCompare(bStr) : bStr.localeCompare(aStr);
    });
  }, [filteredData, sortKey, sortOrder]);

  // Pagination
  const totalPages = Math.max(1, Math.ceil(sortedData.length / pageSize));
  const currentPage = Math.min(Math.max(1, page), totalPages);
  const paginatedData = React.useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return sortedData.slice(start, start + pageSize);
  }, [sortedData, currentPage, pageSize]);

  const handleSort = (key: string) => {
    if (sortKey === key) {
      if (sortOrder === "asc") setSortOrder("desc");
      else {
        setSortKey(null);
        setSortOrder("asc");
      }
    } else {
      setSortKey(key);
      setSortOrder("asc");
    }
  };

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedIds(new Set(paginatedData.map(keyExtractor)));
    } else {
      setSelectedIds(new Set());
    }
  };

  const handleSelectRow = (id: string | number, checked: boolean) => {
    const next = new Set(selectedIds);
    if (checked) next.add(id);
    else next.delete(id);
    setSelectedIds(next);
  };

  const handleExportCsv = () => {
    const exportColumns = columns.filter((c) => visibleColumns.has(String(c.key)));
    const headers = exportColumns.map((c) => `"${c.header}"`).join(",");

    const rows = sortedData.map((item) =>
      exportColumns
        .map((col) => {
          const val = (item as Record<string, unknown>)[col.key as string] ?? "";
          return `"${String(val).replace(/"/g, '""')}"`;
        })
        .join(","),
    );

    const csvContent = "data:text/csv;charset=utf-8," + [headers, ...rows].join("\n");
    const encodedUri = encodeURI(csvContent);
    const link = document.createElement("a");
    link.setAttribute("href", encodedUri);
    link.setAttribute("download", exportFileName);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const isAllSelected =
    paginatedData.length > 0 &&
    paginatedData.every((item) => selectedIds.has(keyExtractor(item)));

  const hasActions = !!(onEdit || onDelete || onView || customActions);

  return (
    <div className={cn("space-y-4", className)}>
      {/* Table Toolbar */}
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3">
        <div className="flex flex-1 items-center gap-2">
          <div className="relative flex-1 max-w-sm">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
              }}
              placeholder={searchPlaceholder}
              className="pl-9 h-9"
            />
          </div>

          {filterOptions.length > 0 && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="h-9 gap-1.5">
                  <Filter className="h-4 w-4" />
                  <span className="hidden sm:inline">Filter</span>
                  {Object.values(activeFilters).filter((v) => v && v !== "all").length > 0 && (
                    <span className="ml-1 rounded-full bg-primary text-primary-foreground px-1.5 py-0.2 text-[10px] font-bold">
                      {Object.values(activeFilters).filter((v) => v && v !== "all").length}
                    </span>
                  )}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-48">
                {filterOptions.map((opt) => (
                  <React.Fragment key={String(opt.key)}>
                    <DropdownMenuLabel>{opt.label}</DropdownMenuLabel>
                    <DropdownMenuItem
                      onClick={() => {
                        setActiveFilters((prev) => ({ ...prev, [String(opt.key)]: "all" }));
                        setPage(1);
                      }}
                    >
                      All
                    </DropdownMenuItem>
                    {opt.options.map((o) => (
                      <DropdownMenuItem
                        key={o.value}
                        onClick={() => {
                          setActiveFilters((prev) => ({ ...prev, [String(opt.key)]: o.value }));
                          setPage(1);
                        }}
                      >
                        {o.label}
                      </DropdownMenuItem>
                    ))}
                    <DropdownMenuSeparator />
                  </React.Fragment>
                ))}
                <DropdownMenuItem
                  onClick={() => {
                    setActiveFilters({});
                    setPage(1);
                  }}
                  className="text-destructive"
                >
                  Clear Filters
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>

        <div className="flex items-center gap-2 justify-end">
          {/* Column Visibility */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" className="h-9 gap-1.5">
                <SlidersHorizontal className="h-4 w-4" />
                <span className="hidden sm:inline">Columns</span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuLabel>Toggle Columns</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {columns.map((col) => (
                <DropdownMenuCheckboxItem
                  key={String(col.key)}
                  checked={visibleColumns.has(String(col.key))}
                  onCheckedChange={(checked) => {
                    const next = new Set(visibleColumns);
                    if (checked) next.add(String(col.key));
                    else if (next.size > 1) next.delete(String(col.key));
                    setVisibleColumns(next);
                  }}
                >
                  {col.header}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Export CSV */}
          <Button variant="outline" size="sm" className="h-9 gap-1.5" onClick={handleExportCsv}>
            <Download className="h-4 w-4" />
            <span className="hidden sm:inline">Export</span>
          </Button>

          {extraHeaderActions}
        </div>
      </div>

      {/* Selected Items Banner */}
      {selectedIds.size > 0 && (
        <div className="flex items-center justify-between rounded-lg bg-muted/60 px-4 py-2 text-xs">
          <span className="font-medium text-foreground">
            {selectedIds.size} row{selectedIds.size > 1 ? "s" : ""} selected
          </span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setSelectedIds(new Set())}
            className="h-6 px-2 text-xs"
          >
            Clear selection
          </Button>
        </div>
      )}

      {/* Data Table */}
      <div className="rounded-xl border bg-card text-card-foreground shadow-sm overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/30">
              <TableHead className="w-10 px-4">
                <Checkbox
                  checked={isAllSelected}
                  onCheckedChange={(checked) => handleSelectAll(!!checked)}
                  aria-label="Select all"
                />
              </TableHead>
              {columns
                .filter((col) => visibleColumns.has(String(col.key)))
                .map((col) => (
                  <TableHead
                    key={String(col.key)}
                    className={cn(
                      "font-semibold",
                      col.sortable && "cursor-pointer select-none",
                    )}
                    onClick={() => col.sortable && handleSort(String(col.key))}
                  >
                    <div className="flex items-center gap-1.5">
                      <span>{col.header}</span>
                      {col.sortable && (
                        <span className="text-muted-foreground">
                          {sortKey === String(col.key) ? (
                            sortOrder === "asc" ? (
                              <ChevronUp className="h-3.5 w-3.5 text-primary" />
                            ) : (
                              <ChevronDown className="h-3.5 w-3.5 text-primary" />
                            )
                          ) : (
                            <ChevronsUpDown className="h-3.5 w-3.5 opacity-50" />
                          )}
                        </span>
                      )}
                    </div>
                  </TableHead>
                ))}
              {hasActions && <TableHead className="w-16 text-right">Actions</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: pageSize }).map((_, idx) => (
                <TableRow key={`skeleton-${idx}`}>
                  <TableCell className="px-4">
                    <Skeleton className="h-4 w-4 rounded" />
                  </TableCell>
                  {columns
                    .filter((col) => visibleColumns.has(String(col.key)))
                    .map((col) => (
                      <TableCell key={String(col.key)}>
                        <Skeleton className="h-4 w-full max-w-[140px] rounded" />
                      </TableCell>
                    ))}
                  {hasActions && (
                    <TableCell>
                      <Skeleton className="h-4 w-6 ml-auto rounded" />
                    </TableCell>
                  )}
                </TableRow>
              ))
            ) : paginatedData.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={
                    columns.filter((col) => visibleColumns.has(String(col.key))).length +
                    (hasActions ? 2 : 1)
                  }
                  className="p-0"
                >
                  <EmptyState
                    title="No records found"
                    description={
                      search
                        ? "No results match your active search filter."
                        : "No records available to display."
                    }
                    className="border-0 rounded-none py-12"
                  />
                </TableCell>
              </TableRow>
            ) : (
              paginatedData.map((item) => {
                const id = keyExtractor(item);
                const isSelected = selectedIds.has(id);

                return (
                  <TableRow
                    key={String(id)}
                    data-state={isSelected ? "selected" : undefined}
                    className={cn(
                      "transition-colors",
                      onRowClick && "cursor-pointer hover:bg-muted/50",
                    )}
                    onClick={() => onRowClick?.(item)}
                  >
                    <TableCell
                      className="px-4"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <Checkbox
                        checked={isSelected}
                        onCheckedChange={(checked) => handleSelectRow(id, !!checked)}
                        aria-label={`Select row ${id}`}
                      />
                    </TableCell>

                    {columns
                      .filter((col) => visibleColumns.has(String(col.key)))
                      .map((col) => (
                        <TableCell key={String(col.key)}>
                          {col.render
                            ? col.render(item)
                            : String((item as Record<string, unknown>)[col.key as string] ?? "")}
                        </TableCell>
                      ))}

                    {hasActions && (
                      <TableCell
                        className="text-right"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <MoreHorizontal className="h-4 w-4" />
                              <span className="sr-only">Open menu</span>
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuLabel>Actions</DropdownMenuLabel>
                            {onView && (
                              <DropdownMenuItem onClick={() => onView(item)}>
                                View Details
                              </DropdownMenuItem>
                            )}
                            {onEdit && (
                              <DropdownMenuItem onClick={() => onEdit(item)}>
                                Edit
                              </DropdownMenuItem>
                            )}
                            {customActions && customActions(item)}
                            {onDelete && (
                              <>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                  onClick={() => onDelete(item)}
                                  className="text-destructive"
                                >
                                  Delete
                                </DropdownMenuItem>
                              </>
                            )}
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    )}
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      {/* Pagination Footer */}
      <div className="flex flex-col sm:flex-row items-center justify-between gap-3 text-xs text-muted-foreground">
        <div>
          Showing {sortedData.length > 0 ? (currentPage - 1) * pageSize + 1 : 0} to{" "}
          {Math.min(currentPage * pageSize, sortedData.length)} of {sortedData.length} records
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPage((p) => Math.max(p - 1, 1))}
            disabled={currentPage <= 1 || isLoading}
            className="h-8 px-3"
          >
            Previous
          </Button>
          <span className="px-2 font-medium text-foreground">
            Page {currentPage} of {totalPages}
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPage((p) => Math.min(p + 1, totalPages))}
            disabled={currentPage >= totalPages || isLoading}
            className="h-8 px-3"
          >
            Next
          </Button>
        </div>
      </div>
    </div>
  );
}
