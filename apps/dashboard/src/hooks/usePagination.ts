import { useState } from "react";
export function usePagination(total: number, pageSize = 10) {
  const [page, setPage] = useState(0);
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  return { page: Math.min(page, pageCount - 1), setPage, pageCount, pageSize };
}
