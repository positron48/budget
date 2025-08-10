import React from "react";
import { render } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";

export function renderWithIntl(
  ui: React.ReactElement,
  { locale = "en", messages = {} as any } = {}
) {
  return render(
    <NextIntlClientProvider locale={locale} messages={messages}>
      {ui}
    </NextIntlClientProvider>
  );
}


