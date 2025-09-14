import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./i18n/request.ts");

const nextConfig: NextConfig = {
  // Add output configuration for better stability
  output: 'standalone',
  // Add some performance optimizations
  poweredByHeader: false,
  compress: true,
  // Set custom port
  env: {
    PORT: '3030',
  },
  async rewrites() {
    const envoyUrl = process.env.ENVOY_URL || "http://localhost:8081";
    return [
      {
        source: "/grpc/:path*",
        destination: `${envoyUrl}/:path*`,
      },
    ];
  },
};

export default withNextIntl(nextConfig);
