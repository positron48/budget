import {authStore, TENANT_CHANGED_EVENT} from "@/lib/auth/store";

// Bind a multi-request task to the selection that started it. The interceptor
// reads the current tenant on every RPC, so cancellation alone is insufficient.
export function tenantRequestScope(tenantId: string, parent?: AbortSignal) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  const check = () => {
    if (parent?.aborted || controller.signal.aborted || authStore.getTenant() !== tenantId) {
      controller.abort();
      throw new DOMException("Tenant request cancelled", "AbortError");
    }
  };
  window.addEventListener(TENANT_CHANGED_EVENT, abort);
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
      parent?.removeEventListener("abort", abort);
    },
  };
}
