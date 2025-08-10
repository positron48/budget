export const transportBaseUrl = process.env.NEXT_PUBLIC_GRPC_BASE_URL ?? "http://localhost:3000/grpc";

export function createTransport(interceptors: any[] = []) {
  const { createGrpcWebTransport } = require("@connectrpc/connect-web");
  return createGrpcWebTransport({ baseUrl: transportBaseUrl, interceptors });
}


