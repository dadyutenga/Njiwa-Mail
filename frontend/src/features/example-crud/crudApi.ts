import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { RecordItem } from "@/types/crud";

export const INITIAL_RECORDS: RecordItem[] = [
  {
    id: "1",
    title: "Quarterly Performance Audit",
    code: "REC-1001",
    category: "Finance",
    status: "active",
    priority: "high",
    assignedTo: "Alex Morgan",
    amount: 14500,
    tags: ["audit", "compliance", "q4"],
    description: "Annual and quarterly internal fiscal assessment review.",
    createdAt: "2024-03-01T08:30:00.000Z",
    updatedAt: "2024-03-15T14:20:00.000Z",
  },
  {
    id: "2",
    title: "Client Portal SSO Integration",
    code: "REC-1002",
    category: "Technology",
    status: "active",
    priority: "urgent",
    assignedTo: "Sarah Chen",
    amount: 8200,
    tags: ["auth", "security", "api"],
    description: "OAuth2 and SAML Single Sign-On implementation.",
    createdAt: "2024-03-03T11:00:00.000Z",
    updatedAt: "2024-03-12T09:45:00.000Z",
  },
  {
    id: "3",
    title: "Brand Asset Redesign",
    code: "REC-1003",
    category: "Marketing",
    status: "pending",
    priority: "medium",
    assignedTo: "David Kim",
    amount: 4500,
    tags: ["design", "branding", "ui"],
    description: "Refreshed color palettes, logos, and design token exports.",
    createdAt: "2024-03-05T14:15:00.000Z",
    updatedAt: "2024-03-10T16:30:00.000Z",
  },
  {
    id: "4",
    title: "Infrastructure Cost Optimization",
    code: "REC-1004",
    category: "Operations",
    status: "active",
    priority: "high",
    assignedTo: "Elena Rossi",
    amount: 18900,
    tags: ["cloud", "devops", "aws"],
    description: "Right-sizing Kubernetes clusters and database instances.",
    createdAt: "2024-03-07T10:00:00.000Z",
    updatedAt: "2024-03-18T11:00:00.000Z",
  },
  {
    id: "5",
    title: "Employee Onboarding Portal",
    code: "REC-1005",
    category: "Human Resources",
    status: "draft",
    priority: "low",
    assignedTo: "Marcus Vance",
    amount: 3200,
    tags: ["hr", "internal", "onboarding"],
    description: "Self-service knowledge base and checklist for new hires.",
    createdAt: "2024-03-08T09:00:00.000Z",
    updatedAt: "2024-03-08T09:00:00.000Z",
  },
  {
    id: "6",
    title: "Data Protection & Privacy Policy",
    code: "REC-1006",
    category: "Operations",
    status: "archived",
    priority: "medium",
    assignedTo: "Alex Morgan",
    amount: 6000,
    tags: ["legal", "gdpr", "compliance"],
    description: "Updated terms of service and GDPR compliance guidelines.",
    createdAt: "2024-02-12T13:00:00.000Z",
    updatedAt: "2024-02-28T17:00:00.000Z",
  },
  {
    id: "7",
    title: "Payment Gateway Fallback Engine",
    code: "REC-1007",
    category: "Technology",
    status: "active",
    priority: "urgent",
    assignedTo: "Sarah Chen",
    amount: 11400,
    tags: ["payments", "reliability", "backend"],
    description: "Automatic retry and fallback routing for failed checkouts.",
    createdAt: "2024-03-10T16:45:00.000Z",
    updatedAt: "2024-03-16T12:00:00.000Z",
  },
  {
    id: "8",
    title: "Global CDN Configuration",
    code: "REC-1008",
    category: "Technology",
    status: "active",
    priority: "medium",
    assignedTo: "Elena Rossi",
    amount: 7500,
    tags: ["cdn", "latency", "performance"],
    description: "Edge caching rules and geo-routing policies setup.",
    createdAt: "2024-03-11T15:20:00.000Z",
    updatedAt: "2024-03-17T08:10:00.000Z",
  },
];

let localRecords = [...INITIAL_RECORDS];

// TanStack Query Hooks
export function useRecordsQuery() {
  return useQuery({
    queryKey: ["records"],
    queryFn: async (): Promise<RecordItem[]> => {
      // Simulate fast network fetch
      await new Promise((r) => setTimeout(r, 200));
      return [...localRecords];
    },
  });
}

export function useRecordQuery(id?: string) {
  return useQuery({
    queryKey: ["records", id],
    queryFn: async (): Promise<RecordItem | null> => {
      await new Promise((r) => setTimeout(r, 150));
      return localRecords.find((r) => r.id === id) || null;
    },
    enabled: !!id,
  });
}

export function useCreateRecordMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (
      newItem: Omit<RecordItem, "id" | "createdAt" | "updatedAt" | "tags">,
    ): Promise<RecordItem> => {
      await new Promise((r) => setTimeout(r, 300));
      const record: RecordItem = {
        ...newItem,
        id: String(Date.now()),
        tags: [newItem.category.toLowerCase()],
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      };
      localRecords = [record, ...localRecords];
      return record;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["records"] });
    },
  });
}

export function useUpdateRecordMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async ({
      id,
      updates,
    }: {
      id: string;
      updates: Partial<RecordItem>;
    }): Promise<RecordItem> => {
      await new Promise((r) => setTimeout(r, 300));
      const idx = localRecords.findIndex((r) => r.id === id);
      if (idx === -1) throw new Error("Record not found");

      const updated: RecordItem = {
        ...localRecords[idx],
        ...updates,
        updatedAt: new Date().toISOString(),
      };
      localRecords[idx] = updated;
      return updated;
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ["records"] });
      queryClient.invalidateQueries({ queryKey: ["records", variables.id] });
    },
  });
}

export function useDeleteRecordMutation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await new Promise((r) => setTimeout(r, 250));
      localRecords = localRecords.filter((r) => r.id !== id);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["records"] });
    },
  });
}
