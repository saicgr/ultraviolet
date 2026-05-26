// Phase 1 simplification: a single "active" customer for the dashboard. The auth/JWT layer
// resolves the real customer from the session in Phase 1.5.
import { useQuery } from "@tanstack/react-query";
import { api, Customer } from "./api";

export function useActiveCustomer() {
  return useQuery<Customer | null>({
    queryKey: ["active-customer"],
    queryFn: async () => {
      const all = await api.listCustomers();
      return all[0] ?? null;
    },
  });
}
