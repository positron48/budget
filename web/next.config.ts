import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./i18n/request.ts");

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      {
        source: "/grpc/:path*",
        destination: "http://localhost:8081/:path*",
      },
    ];
  },
};

export default withNextIntl(nextConfig);
