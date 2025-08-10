import {NextRequest, NextResponse} from 'next/server';

const SUPPORTED = ['en', 'ru'];

export default function middleware(req: NextRequest) {
  const {pathname} = req.nextUrl;
  // Handle explicit locale prefix by redirecting to unprefixed path and set cookie
  const m = pathname.match(/^\/(en|ru)(\/.*)?$/);
  if (m) {
    const loc = m[1];
    const rest = m[2] ?? '';
    const url = req.nextUrl.clone();
    url.pathname = rest || '/';
    const res = NextResponse.redirect(url);
    res.cookies.set('NEXT_LOCALE', loc, {path: '/'});
    res.headers.set('X-NEXT-INTL-LOCALE', loc);
    return res;
  }
  // Otherwise, ensure intl header is present based on cookie or default
  const locCookie = req.cookies.get('NEXT_LOCALE')?.value;
  const locale = SUPPORTED.includes(locCookie ?? '') ? (locCookie as string) : 'en';
  const res = NextResponse.next();
  res.headers.set('X-NEXT-INTL-LOCALE', locale);
  return res;
}

export const config = {
  matcher: [
    '/((?!api|_next|.*\\..*).*)'
  ]
};


