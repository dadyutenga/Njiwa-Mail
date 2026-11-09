export type ItemStatus = "active" | "pending" | "archived" | "draft";
export type ItemPriority = "low" | "medium" | "high" | "urgent";

export interface RecordItem {
  id: string;
  title: string;
  code: string;
  category: string;
  status: ItemStatus;
  priority: ItemPriority;
  assignedTo: string;
  amount: number;
  tags: string[];
  description?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CrudFilters {
  search: string;
  status?: ItemStatus | "all";
  category?: string;
  priority?: ItemPriority | "all";
}
