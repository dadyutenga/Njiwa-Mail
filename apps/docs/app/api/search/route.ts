import { createFromSource } from "fumadocs-core/search/server";
import { source } from "@/lib/source";

// Exported statically: the search index is a file, not a server.
export const revalidate = false;
export const { staticGET: GET } = createFromSource(source);
