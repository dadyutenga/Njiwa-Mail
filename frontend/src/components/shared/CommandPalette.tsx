import * as React from "react";
import { useNavigate } from "react-router-dom";
import {
  LayoutDashboard,
  FileSpreadsheet,
  BarChart3,
  Settings,
  User,
  Moon,
  Sun,
  Laptop,
  HelpCircle,
  LogOut,
} from "lucide-react";
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

export interface CommandPaletteProps {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}

export function CommandPalette({ open: externalOpen, onOpenChange }: CommandPaletteProps) {
  const [internalOpen, setInternalOpen] = React.useState(false);
  const navigate = useNavigate();
  const { setTheme } = useTheme();
  const { logout } = useAuth();

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
          <CommandItem
            onSelect={() => handleSelect(() => navigate("/dashboard"))}
            className="gap-2"
          >
            <LayoutDashboard className="h-4 w-4 text-muted-foreground" />
            <span>Dashboard</span>
          </CommandItem>
          <CommandItem
            onSelect={() => handleSelect(() => navigate("/messages"))}
            className="gap-2"
          >
            <FileSpreadsheet className="h-4 w-4 text-muted-foreground" />
            <span>Messages</span>
          </CommandItem>
          <CommandItem
            onSelect={() => handleSelect(() => navigate("/projects"))}
            className="gap-2"
          >
            <FileSpreadsheet className="h-4 w-4 text-muted-foreground" />
            <span>Projects</span>
          </CommandItem>
          <CommandItem
            onSelect={() => handleSelect(() => navigate("/reports"))}
            className="gap-2"
          >
            <BarChart3 className="h-4 w-4 text-muted-foreground" />
            <span>Reports & Analytics</span>
          </CommandItem>
          <CommandItem
            onSelect={() => handleSelect(() => navigate("/example"))}
            className="gap-2"
          >
            <HelpCircle className="h-4 w-4 text-muted-foreground" />
            <span>How to Add a Page</span>
          </CommandItem>
        </CommandGroup>

        <CommandSeparator />

        <CommandGroup heading="User & Settings">
          <CommandItem
            onSelect={() => handleSelect(() => navigate("/profile"))}
            className="gap-2"
          >
            <User className="h-4 w-4 text-muted-foreground" />
            <span>Profile</span>
          </CommandItem>
          <CommandItem
            onSelect={() => handleSelect(() => navigate("/settings"))}
            className="gap-2"
          >
            <Settings className="h-4 w-4 text-muted-foreground" />
            <span>Settings</span>
          </CommandItem>
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
