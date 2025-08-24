export const transportBaseUrl = process.env.NEXT_PUBLIC_GRPC_BASE_URL;
if (!transportBaseUrl) {
  throw new Error("NEXT_PUBLIC_GRPC_BASE_URL environment variable is required");
}

export function createTransport(interceptors: any[] = []) {
  const { createGrpcWebTransport } = require("@connectrpc/connect-web");
  return createGrpcWebTransport({ baseUrl: transportBaseUrl, interceptors });
}


