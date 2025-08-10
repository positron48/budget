import type { NextConfig } from "next";

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

export default nextConfig;
