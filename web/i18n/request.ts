import {getRequestConfig} from 'next-intl/server';

export default getRequestConfig(async ({locale}) => {
  if (!['en', 'ru'].includes(locale)) locale = 'en';
  const messages = locale === 'ru' ? (await import('./../i18n/ru.json')).default : (await import('./../i18n/en.json')).default;
  return {locale, messages} as any;
});


