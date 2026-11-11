import { useI18n } from "@/features/i18n/i18n";
import { content } from "./content";
import { contentSw } from "./content.sw";

export function useWelcomeContent() {
  const { locale } = useI18n();
  return locale === "sw" ? contentSw : content;
}
