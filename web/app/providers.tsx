"use client";

import { createContext, useContext, useMemo, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createTransport } from "@/lib/api/transport";
import { createClients } from "@/lib/api/clients";
import { authStore } from "@/lib/auth/store";
import { authInterceptor, tenantInterceptor, loggingInterceptor } from "@/lib/api/interceptors";

const ClientsContext = createContext<any>(null);

export function ClientsProvider({ children }: { children: React.ReactNode }) {
  const [queryClient] = useState(() => new QueryClient());
  const interceptors = useMemo(
    () => [
      loggingInterceptor(),
      authInterceptor(() => authStore.getAccess()),
      tenantInterceptor(() => authStore.getTenant()),
    ],
    []
  );
  const transport = useMemo(() => createTransport(interceptors), [interceptors]);
  const clients = useMemo(() => createClients(transport) as any, [transport]);
  return (
    <QueryClientProvider client={queryClient}>
      <ClientsContext.Provider value={clients}>{children}</ClientsContext.Provider>
    </QueryClientProvider>
  );
}

export function useClients(): any {
  const ctx = useContext(ClientsContext);
  if (!ctx) throw new Error("ClientsProvider missing");
  return ctx;
}


