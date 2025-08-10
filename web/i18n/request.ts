import {getRequestConfig} from 'next-intl/server';

export default getRequestConfig(async ({requestLocale}) => {
  const loc = await requestLocale;
  const safe = (loc && ['en', 'ru'].includes(loc)) ? loc : 'en';
  const messages = (await import(`./../i18n/${safe}.json`)).default;
  return {locale: safe, messages} as any;
});


