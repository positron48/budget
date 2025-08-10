"use client";

import { ClientsProvider } from "@/app/providers";

function ProfileSettingsInner() {
  return (
    <div className="p-6">
      <h1 className="text-2xl font-semibold">Profile</h1>
      <p className="text-sm text-gray-500">Update name/locale and change password.</p>
    </div>
  );
}

export default function ProfileSettingsPage() {
  return (
    <ClientsProvider>
      <ProfileSettingsInner />
    </ClientsProvider>
  );
}

