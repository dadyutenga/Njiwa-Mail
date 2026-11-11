import { Globe } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useI18n } from "./i18n";

export function LanguageSwitcher() {
  const { locale, setLocale } = useI18n();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="gap-1.5 px-2 text-xs font-medium"
          aria-label={`Current language: ${locale === "sw" ? "Kiswahili" : "English"}. Switch language`}
        >
          <Globe size={16} />
          <span className="uppercase">{locale}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          className={locale === "en" ? "font-semibold text-primary" : ""}
          onClick={() => setLocale("en")}
        >
          English (EN)
        </DropdownMenuItem>
        <DropdownMenuItem
          className={locale === "sw" ? "font-semibold text-primary" : ""}
          onClick={() => setLocale("sw")}
        >
          Kiswahili (SW)
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
