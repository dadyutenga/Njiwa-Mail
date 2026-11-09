import * as React from "react";
import { useNavigate } from "react-router-dom";
import { Moon, Sun, Laptop, LogOut } from "lucide-react";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";
import { useTheme } from "@/contexts/ThemeContext";
import { useAuth } from "@/contexts/AuthContext";
import { getNavSections } from "@/app/routes.config";

export interface CommandPaletteProps {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export function CommandPalette({ open: externalOpen, onOpenChange }: CommandPaletteProps) {
  const [internalOpen, setInternalOpen] = React.useState(false);
  const navigate = useNavigate();
  const { setTheme } = useTheme();
  const { logout, user } = useAuth();
  const navItems = getNavSections(user?.role).flatMap((section) => section.items);

  const isOpen = externalOpen !== undefined ? externalOpen : internalOpen;
  const setOpen = onOpenChange || setInternalOpen;

  React.useEffect(() => {
    const down = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen(!isOpen);
      }
    };

    document.addEventListener("keydown", down);
    return () => document.removeEventListener("keydown", down);
  }, [isOpen, setOpen]);

  const handleSelect = (callback: () => void) => {
    setOpen(false);
    callback();
  };

  return (
    <CommandDialog open={isOpen} onOpenChange={setOpen}>
      <CommandInput placeholder="Type a command or search..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>

        <CommandGroup heading="Navigation">
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <CommandItem
                key={item.path}
                onSelect={() => handleSelect(() => navigate(item.path))}
                className="gap-2"
              >
                {Icon && <Icon className="h-4 w-4 text-muted-foreground" />}
                <span>{item.title}</span>
              </CommandItem>
            );
          })}
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Theme">
          <CommandItem onSelect={() => handleSelect(() => setTheme("light"))} className="gap-2">
            <Sun className="h-4 w-4 text-muted-foreground" />
            <span>Set Light Theme</span>
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme("dark"))} className="gap-2">
            <Moon className="h-4 w-4 text-muted-foreground" />
            <span>Set Dark Theme</span>
          </CommandItem>
          <CommandItem onSelect={() => handleSelect(() => setTheme("system"))} className="gap-2">
            <Laptop className="h-4 w-4 text-muted-foreground" />
            <span>Set System Theme</span>
          </CommandItem>
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="Account">
          <CommandItem
            onSelect={() =>
              handleSelect(() => {
                logout();
                navigate("/login");
              })
            }
            className="gap-2 text-destructive focus:text-destructive"
          >
            <LogOut className="h-4 w-4" />
            <span>Sign Out</span>
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  );
}
