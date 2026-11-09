import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";
import type {
  ConsoleKey,
  ConsoleOverview,
  ConsoleProject,
  MailMessage,
} from "@/types/mail";

export function useOverviewQuery() {
  return useQuery({
    queryKey: ["console", "overview"],
    queryFn: async () => {
      const response = await apiClient.get<ConsoleOverview>("/overview");
      return response.data;
    },
    refetchInterval: 15_000,
  });
}

export function useMessagesQuery() {
  return useQuery({
    queryKey: ["console", "messages"],
    queryFn: async () => {
      const response = await apiClient.get<{ messages: MailMessage[] }>("/messages");
      return response.data.messages;
    },
    refetchInterval: 15_000,
  });
}

export function useMessageQuery(id: string | undefined) {
  return useQuery({
    queryKey: ["console", "message", id],
    enabled: Boolean(id),
    queryFn: async () => {
      const response = await apiClient.get<MailMessage>(`/messages/${id}`);
      return response.data;
    },
  });
}

export function useProjectsQuery() {
  return useQuery({
    queryKey: ["console", "projects"],
    queryFn: async () => {
      const response = await apiClient.get<{ projects: ConsoleProject[] }>("/projects");
      return response.data.projects;
    },
  });
}

export function useKeysQuery() {
  return useQuery({
    queryKey: ["console", "keys"],
    queryFn: async () => {
      const response = await apiClient.get<{ keys: ConsoleKey[] }>("/keys");
      return response.data.keys;
    },
  });
}
