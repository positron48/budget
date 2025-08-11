import { Code } from "@connectrpc/connect";

export function normalizeApiErrorMessage(err: any, fallback: string): string {
  const code: number | undefined = err?.code;
  let message: string = String(err?.message ?? "").trim();

  if (code === Code.NotFound) return "Запись не найдена";
  if (code === Code.InvalidArgument) return "Некорректные данные";
  if (code === Code.AlreadyExists) return "Уже существует";
  if (code === Code.Unauthenticated) return "Требуется авторизация";

  if (message) {
    // remove leading [tag] prefixes like [not found], [internal]
    message = message.replace(/^\[[^\]]+\]\s*/i, "");
  }

  return message || fallback;
}


