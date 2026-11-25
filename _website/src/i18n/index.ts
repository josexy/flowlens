import { en, type Dictionary } from './en';
import { zhCn } from './zh-cn';

export { format } from '../lib/text';

export type Locale = 'en' | 'zh-cn';
export type { Dictionary };

export const LOCALES: readonly Locale[] = ['en', 'zh-cn'];

const dictionaries: Record<Locale, Dictionary> = { en, 'zh-cn': zhCn };

export function dictionary(locale: Locale): Dictionary {
  return dictionaries[locale];
}

/** Path of a locale's homepage below the site base (`/` or `/zh-cn/`). */
export function homePath(locale: Locale, base: string = import.meta.env.BASE_URL): string {
  const root = base.endsWith('/') ? base : `${base}/`;
  return locale === 'en' ? root : `${root}${locale}/`;
}
