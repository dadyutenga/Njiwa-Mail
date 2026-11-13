import { defineConfig, defineDocs, frontmatterSchema } from "fumadocs-mdx/config";
import { z } from "zod";

export const docs = defineDocs({
  dir: "content/docs",
  docs: {
    schema: frontmatterSchema.extend({
      // PLANNED pages describe capabilities that are not implemented yet.
      status: z.enum(["stable", "planned"]).default("stable"),
    }),
  },
});

export default defineConfig({
  mdxOptions: {
    // High-contrast code themes: the default light theme has tokens below WCAG AA contrast.
    rehypeCodeOptions: { themes: { light: "github-light-high-contrast", dark: "github-dark-high-contrast" } },
  },
});
