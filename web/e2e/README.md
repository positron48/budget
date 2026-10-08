# Browser page smoke tests

Run `npm ci`, `npx playwright install chromium`, then:

```
NEXT_PUBLIC_GRPC_BASE_URL=http://127.0.0.1:3030/grpc npm run build
npm run test:e2e
```

The suite discovers every `app/**/page.tsx` and opens it in Chromium in English
and Russian. It fails on uncaught JavaScript exceptions, console errors, missing
page content, or unexpected redirects. Dynamic account routes use a fixture ID;
`/tenants` must redirect to `/account`. New dynamic parameters require fixtures.

RPC responses are protobuf binary fixtures intercepted at the network boundary.
Real page components, layout, providers, React Query and gRPC-Web decoding run.
The FX test additionally checks loaded rates and a date change. This catches
query-key serialization failures without a production login or database writes.
It does not verify backend authorization or all user interactions.

CI runs these tests against the production build before publishing Docker images
and uploads Playwright traces on failure. To check deployed frontend bundles
with the same isolated RPC fixtures, set `E2E_BASE_URL=https://budget.qantrix.ru`.
