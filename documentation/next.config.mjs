import { createMDX } from "fumadocs-mdx/next";

const withMDX = createMDX();

/** @type {import('next').NextConfig} */
const config = {
  // A static export: no server runtime, nothing to proxy, nothing secret in the image.
  output: "export",
  reactStrictMode: true,
  images: { unoptimized: true },
  trailingSlash: false,
};

export default withMDX(config);
