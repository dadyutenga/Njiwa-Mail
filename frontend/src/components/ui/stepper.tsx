import * as React from "react";
import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

export interface Step {
  id: string | number;
  title: string;
  description?: string;
}

export interface StepperProps {
  steps: Step[];
  currentStep: number;
  onStepClick?: (stepIndex: number) => void;
  className?: string;
}

export function Stepper({
  steps,
  currentStep,
  onStepClick,
  className,
}: StepperProps) {
  return (
    <nav aria-label="Progress" className={cn("w-full", className)}>
      <ol className="flex items-center justify-between">
        {steps.map((step, index) => {
          const isCompleted = index < currentStep;
          const isCurrent = index === currentStep;
          const isClickable = onStepClick && index <= currentStep;

          return (
            <li
              key={step.id}
              className={cn(
                "relative flex-1",
                index !== steps.length - 1 && "pr-4 sm:pr-8",
              )}
            >
              <div className="flex items-center">
                <button
                  type="button"
                  disabled={!isClickable}
                  onClick={() => onStepClick?.(index)}
                  className={cn(
                    "flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-sm font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2",
                    isCompleted && "bg-primary text-primary-foreground",
                    isCurrent && "border-2 border-primary bg-background text-primary",
                    !isCompleted && !isCurrent && "border-2 border-muted bg-muted/40 text-muted-foreground",
                    !isClickable && "cursor-default",
                  )}
                  aria-current={isCurrent ? "step" : undefined}
                >
                  {isCompleted ? (
                    <Check className="h-4 w-4" />
                  ) : (
                    <span>{index + 1}</span>
                  )}
                </button>
                {index !== steps.length - 1 && (
                  <div
                    className={cn(
                      "ml-2 sm:ml-4 h-0.5 flex-1 transition-colors",
                      isCompleted ? "bg-primary" : "bg-muted",
                    )}
                  />
                )}
              </div>
              <div className="mt-2">
                <p
                  className={cn(
                    "text-xs font-medium",
                    isCurrent && "text-primary font-semibold",
                    !isCurrent && "text-muted-foreground",
                  )}
                >
                  {step.title}
                </p>
                {step.description && (
                  <p className="text-[11px] text-muted-foreground/80 hidden sm:block">
                    {step.description}
                  </p>
                )}
              </div>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
