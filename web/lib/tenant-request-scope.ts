import {authStore, TENANT_CHANGED_EVENT, AUTH_CHANGED_EVENT} from "@/lib/auth/store";

// Bind a multi-request task to the selection that started it. The interceptor
// reads the current tenant on every RPC, so cancellation alone is insufficient.
export function tenantRequestScope(tenantId: string, parent?: AbortSignal, actorId?: string) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  const check = () => {
    if (parent?.aborted || controller.signal.aborted || authStore.getTenant() !== tenantId || actorId !== undefined && authStore.getActor() !== actorId) {
      controller.abort();
      throw new DOMException("Tenant request cancelled", "AbortError");
    }
  };
  const authChanged = () => {if (actorId !== undefined && authStore.getActor() !== actorId) abort();};
  window.addEventListener(TENANT_CHANGED_EVENT, abort);
  window.addEventListener(AUTH_CHANGED_EVENT, authChanged);
  parent?.addEventListener("abort", abort, {once: true});
  return {
    signal: controller.signal,
    check,
    async run<T>(request: (options: {signal: AbortSignal}) => Promise<T>): Promise<T> {
      check();
      const result = await request({signal: controller.signal});
      check();
      return result;
    },
    dispose() {
      controller.abort();
      window.removeEventListener(TENANT_CHANGED_EVENT, abort);
      window.removeEventListener(AUTH_CHANGED_EVENT, authChanged);
      parent?.removeEventListener("abort", abort);
    },
  };
}
