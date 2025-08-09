export const transportBaseUrl = process.env.NEXT_PUBLIC_GRPC_BASE_URL ?? "http://localhost:8081";

export function createTransport() {
  const { createConnectTransport } = require("@connectrpc/connect-web");
  return createConnectTransport({ baseUrl: transportBaseUrl });
}


