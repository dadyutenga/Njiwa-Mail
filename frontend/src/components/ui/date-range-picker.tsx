import * as React from "react";
import { format, subDays } from "date-fns";
import { Calendar as CalendarIcon } from "lucide-react";
import type { DateRange } from "react-day-picker";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

export interface DateRangePickerProps {
  dateRange?: DateRange;
  onSelect?: (range: DateRange | undefined) => void;
  className?: string;
  showPresets?: boolean;
}

export function DateRangePicker({
  dateRange,
  onSelect,
  className,
  showPresets = true,
}: DateRangePickerProps) {
  const [internalRange, setInternalRange] = React.useState<DateRange | undefined>(
    dateRange || {
      from: subDays(new Date(), 30),
      to: new Date(),
    },
  );

  const handleSelect = (range: DateRange | undefined) => {
    setInternalRange(range);
    onSelect?.(range);
  };

  const applyPreset = (days: number) => {
    const range: DateRange = {
      from: subDays(new Date(), days),
      to: new Date(),
    };
    handleSelect(range);
  };

  const currentRange = dateRange || internalRange;

  return (
    <div className={cn("grid gap-2", className)}>
      <Popover>
        <PopoverTrigger asChild>
          <Button
            id="date"
            variant={"outline"}
            className={cn(
              "justify-start text-left font-normal h-10 w-full sm:w-[280px]",
              !currentRange && "text-muted-foreground",
            )}
          >
            <CalendarIcon className="mr-2 h-4 w-4" />
            {currentRange?.from ? (
              currentRange.to ? (
                <>
                  {format(currentRange.from, "LLL dd, y")} -{" "}
                  {format(currentRange.to, "LLL dd, y")}
                </>
              ) : (
                format(currentRange.from, "LLL dd, y")
              )
            ) : (
              <span>Pick date range</span>
            )}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="end">
          {showPresets && (
            <div className="flex border-b p-2 gap-1 justify-center bg-muted/40">
              <Button
                variant="ghost"
                size="sm"
                className="text-xs h-7 px-2"
                onClick={() => applyPreset(7)}
              >
                Last 7d
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="text-xs h-7 px-2"
                onClick={() => applyPreset(30)}
              >
                Last 30d
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="text-xs h-7 px-2"
                onClick={() => applyPreset(90)}
              >
                Last 90d
              </Button>
            </div>
          )}
          <Calendar
            initialFocus
            mode="range"
            defaultMonth={currentRange?.from}
            selected={currentRange}
            onSelect={handleSelect}
            numberOfMonths={2}
          />
        </PopoverContent>
      </Popover>
    </div>
  );
}
