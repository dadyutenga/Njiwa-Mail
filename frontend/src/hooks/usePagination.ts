import { useMemo, useState } from "react";

export interface UsePaginationOptions {
  totalItems: number;
  initialPage?: number;
  initialPageSize?: number;
}

export interface UsePaginationResult {
  page: number;
  pageSize: number;
  totalPages: number;
  startIndex: number;
  endIndex: number;
  hasNextPage: boolean;
  hasPrevPage: boolean;
  setPage: (page: number) => void;
  setPageSize: (size: number) => void;
  nextPage: () => void;
  prevPage: () => void;
  firstPage: () => void;
  lastPage: () => void;
}

export function usePagination({
  totalItems,
  initialPage = 1,
  initialPageSize = 10,
}: UsePaginationOptions): UsePaginationResult {
  const [page, setPage] = useState(initialPage);
  const [pageSize, setPageSize] = useState(initialPageSize);

  const totalPages = useMemo(() => {
    return Math.max(1, Math.ceil(totalItems / pageSize));
  }, [totalItems, pageSize]);

  const safePage = Math.min(Math.max(1, page), totalPages);

  const startIndex = (safePage - 1) * pageSize;
  const endIndex = Math.min(startIndex + pageSize, totalItems);

  return {
    page: safePage,
    pageSize,
    totalPages,
    startIndex,
    endIndex,
    hasNextPage: safePage < totalPages,
    hasPrevPage: safePage > 1,
    setPage: (newPage: number) => setPage(Math.min(Math.max(1, newPage), totalPages)),
    setPageSize: (newSize: number) => {
      setPageSize(newSize);
      setPage(1);
    },
    nextPage: () => setPage((p) => Math.min(p + 1, totalPages)),
    prevPage: () => setPage((p) => Math.max(p - 1, 1)),
    firstPage: () => setPage(1),
    lastPage: () => setPage(totalPages),
  };
}
