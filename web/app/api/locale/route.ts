import { NextRequest, NextResponse } from "next/server";

export async function POST(req: NextRequest) {
  const data = await req.json().catch(() => ({}));
  const loc = typeof data?.locale === "string" ? data.locale : "en";
  const locale = loc === "ru" ? "ru" : "en";
  const res = NextResponse.json({ ok: true, locale });
  res.cookies.set("NEXT_LOCALE", locale, { path: "/", maxAge: 60 * 60 * 24 * 365 });
  res.headers.set("X-NEXT-INTL-LOCALE", locale);
  return res;
}


