import { useMutation, useQuery } from "@tanstack/react-query";
import { apiRequest } from "./api";
type Example = { id: string; name: string };
/** These hooks only call an API when used by your application. */
export function useExampleQuery() {
  return useQuery({ queryKey: ["example"], queryFn: () => apiRequest<Example[]>("/example") });
}
export function useExampleMutation() {
  return useMutation({
    mutationFn: (data: Example) =>
      apiRequest<Example>("/example", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(data),
      }),
  });
}
