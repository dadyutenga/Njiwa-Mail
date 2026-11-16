import type { Metadata } from "next";
import { notFound } from "next/navigation";
import defaultMdxComponents from "fumadocs-ui/mdx";
import { Step, Steps } from "fumadocs-ui/components/steps";
import { Tab, Tabs } from "fumadocs-ui/components/tabs";
import { DocsBody, DocsDescription, DocsPage, DocsTitle } from "fumadocs-ui/layouts/docs/page";
import Link from "next/link";
import { getBreadcrumbItems } from "fumadocs-core/breadcrumb";
import { PageActions } from "@/components/page-actions";
import { source } from "@/lib/source";

export default async function Page(props: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await props.params;
  const page = source.getPage(slug);
  if (!page) notFound();
  const MDX = page.data.body;
  const crumbs = getBreadcrumbItems(page.url, source.getPageTree(), { includePage: true });
  const md = (slug && slug.length ? `/docs/${slug.join("/")}` : "/docs/index") + ".md";
  return (
    <DocsPage toc={page.data.toc} breadcrumb={{ enabled: false }}>
      <nav aria-label="Breadcrumb" className="mb-2 text-sm text-fd-muted-foreground">
        <ol className="flex flex-wrap items-center gap-1.5">
          <li>
            <Link href="/docs" className="hover:underline">
              Docs
            </Link>
          </li>
          {crumbs
            .filter((c) => c.url !== "/docs")
            .map((c, i, all) => (
              <li key={i} className="flex items-center gap-1.5" aria-current={i === all.length - 1 ? "page" : undefined}>
                <span aria-hidden="true">/</span>
                {c.url && i < all.length - 1 ? (
                  <Link href={c.url} className="hover:underline">
                    {c.name}
                  </Link>
                ) : (
                  <span>{c.name}</span>
                )}
              </li>
            ))}
        </ol>
      </nav>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      {page.data.status === "planned" && (
        <p role="note" className="mb-4 rounded-lg border border-amber-500/50 bg-amber-50 px-3 py-2 text-sm text-amber-950 dark:bg-amber-950/40 dark:text-amber-100">
          <strong>PLANNED.</strong> This page describes something that is not implemented yet. Do not build against it.
        </p>
      )}
      <PageActions markdownPath={md} />
      <DocsBody>
        <MDX components={{ ...defaultMdxComponents, Tabs, Tab, Steps, Step }} />
      </DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: { params: Promise<{ slug?: string[] }> }): Promise<Metadata> {
  const { slug } = await props.params;
  const page = source.getPage(slug);
  if (!page) notFound();
  return { title: page.data.title, description: page.data.description };
}
