"use client";

import { ClientsProvider, useClients } from "@/app/providers";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { authStore } from "@/lib/auth/store";

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(6),
  name: z.string().min(1),
  locale: z.string().default("ru"),
  tenantName: z.string().min(1),
});
type FormValues = z.infer<typeof schema>;

function RegisterForm() {
  const { auth } = useClients();
  const { register, handleSubmit, formState } = useForm<FormValues>({ resolver: zodResolver(schema) });
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const onSubmit = async (values: FormValues) => {
    setLoading(true);
    setError(null);
    try {
      const res = await auth.register(values as any);
      const access = res.tokens?.accessToken ?? "";
      const refresh = res.tokens?.refreshToken ?? "";
      authStore.set({ accessToken: access, refreshToken: refresh });
      const tenantId = res.tenant?.id as unknown as string | undefined;
      if (tenantId) authStore.set({ tenantId });
      window.location.href = "/";
    } catch (e: any) {
      setError(e?.message ?? "Registration failed");
    } finally {
      setLoading(false);
    }
  };
  return (
    <div className="p-6 max-w-md mx-auto">
      <h1 className="text-2xl font-semibold mb-4">Register</h1>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-3">
        <div>
          <label className="block text-sm">Email</label>
          <input className="border rounded px-3 py-2 w-full" {...register("email")} />
          {formState.errors.email && <div className="text-xs text-red-600">{formState.errors.email.message}</div>}
        </div>
        <div>
          <label className="block text-sm">Password</label>
          <input type="password" className="border rounded px-3 py-2 w-full" {...register("password")} />
          {formState.errors.password && <div className="text-xs text-red-600">{formState.errors.password.message}</div>}
        </div>
        <div>
          <label className="block text-sm">Name</label>
          <input className="border rounded px-3 py-2 w-full" {...register("name")} />
        </div>
        <div>
          <label className="block text-sm">Locale</label>
          <input className="border rounded px-3 py-2 w-full" defaultValue="ru" {...register("locale")} />
        </div>
        <div>
          <label className="block text-sm">Tenant Name</label>
          <input className="border rounded px-3 py-2 w-full" {...register("tenantName")} />
        </div>
        {error && <div className="text-red-600 text-sm">{error}</div>}
        <button className="bg-black text-white rounded px-4 py-2" disabled={loading}>
          {loading ? "Creating..." : "Create account"}
        </button>
      </form>
    </div>
  );
}

export default function RegisterPage() {
  return (
    <ClientsProvider>
      <RegisterForm />
    </ClientsProvider>
  );
}


