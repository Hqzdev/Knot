import type { NextConfig } from "next";

const developmentServices = {
  api: process.env.KNOT_DEV_API_ORIGIN ?? "http://127.0.0.1:8080",
  gateway: process.env.KNOT_DEV_GATEWAY_ORIGIN ?? "http://127.0.0.1:8086",
  presence: process.env.KNOT_DEV_PRESENCE_ORIGIN ?? "http://127.0.0.1:8083",
  attachments: process.env.KNOT_DEV_ATTACHMENTS_ORIGIN ?? "http://127.0.0.1:8082",
  preview: process.env.KNOT_DEV_PREVIEW_ORIGIN ?? "http://127.0.0.1:8087",
};

const nextConfig: NextConfig = {
  devIndicators: false,
  output: "standalone",
  outputFileTracingRoot: process.cwd(),
  reactStrictMode: true,
  turbopack: { root: process.cwd() },
  async rewrites() {
    if (process.env.NODE_ENV !== "development") {
      return [];
    }
    return Object.entries(developmentServices).map(([service, origin]) => ({
      source: `/${service}/:path*`,
      destination: `${origin}/:path*`,
    }));
  },
};

export default nextConfig;
