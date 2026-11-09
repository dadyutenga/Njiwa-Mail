import * as React from "react";
import { Layers, Sparkles } from "lucide-react";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/shared/CopyButton";
import { Stepper } from "@/components/ui/stepper";

export function ExamplePage() {
  const [activeStep, setActiveStep] = React.useState(0);
  const [count, setCount] = React.useState(0);

  const steps = [
    { id: 1, title: "Create Page File", description: "Write React component" },
    { id: 2, title: "Register in Route Config", description: "Add single config item" },
    { id: 3, title: "Done & Ready to Ship", description: "Nav & route auto-generate" },
  ];

  const pageCodeSnippet = `// src/pages/AnalyticsOverview.tsx
import * as React from "react";
import { StatCard } from "@/components/shared/StatCard";

export function AnalyticsOverview() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Analytics Overview</h1>
      <StatCard title="Active Sessions" value="1,240" change={12.5} />
    </div>
  );
}

export default AnalyticsOverview;`;

  const configSnippet = `// src/app/routes.config.tsx
import { BarChart3 } from "lucide-react";
import { lazy } from "react";

const AnalyticsOverview = lazy(() => import("@/pages/AnalyticsOverview"));

// Inside routesConfig array:
{
  path: "/analytics",
  title: "Analytics",
  icon: BarChart3,
  element: <AnalyticsOverview />,
  layout: "dashboard",
  inNav: true,
  navSection: "main",
},`;

  return (
    <div className="space-y-6 max-w-4xl">
      <div>
        <div className="flex items-center gap-2">
          <span className="rounded-md bg-primary/10 p-1.5 text-primary">
            <Sparkles className="h-5 w-5" />
          </span>
          <h1 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">
            How to Add a New Page
          </h1>
        </div>
        <p className="mt-1 text-sm text-muted-foreground">
          Follow this 2-step blueprint to add a new authenticated page with automatic sidebar navigation and breadcrumbs in seconds.
        </p>
      </div>

      <Card className="p-6">
        <Stepper
          steps={steps}
          currentStep={activeStep}
          onStepClick={(step) => setActiveStep(step)}
        />
      </Card>

      {/* Step 1 */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <span className="flex h-7 w-7 items-center justify-center rounded-full bg-primary text-xs font-bold text-primary-foreground">
                1
              </span>
              <div>
                <CardTitle className="text-base">Create your Page component</CardTitle>
                <CardDescription>
                  Create a new file in <code>src/pages/MyNewPage.tsx</code>.
                </CardDescription>
              </div>
            </div>
            <CopyButton value={pageCodeSnippet} label="Copy Code" />
          </div>
        </CardHeader>
        <CardContent>
          <pre className="overflow-x-auto rounded-lg bg-muted p-4 text-xs font-mono text-foreground leading-relaxed">
            {pageCodeSnippet}
          </pre>
        </CardContent>
      </Card>

      {/* Step 2 */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <span className="flex h-7 w-7 items-center justify-center rounded-full bg-primary text-xs font-bold text-primary-foreground">
                2
              </span>
              <div>
                <CardTitle className="text-base">Register in routes.config.tsx</CardTitle>
                <CardDescription>
                  Add a single config object to the unified navigation array.
                </CardDescription>
              </div>
            </div>
            <CopyButton value={configSnippet} label="Copy Snippet" />
          </div>
        </CardHeader>
        <CardContent>
          <pre className="overflow-x-auto rounded-lg bg-muted p-4 text-xs font-mono text-foreground leading-relaxed">
            {configSnippet}
          </pre>
        </CardContent>
      </Card>

      {/* Interactive Demo Component */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base flex items-center gap-2">
            <Layers className="h-4 w-4 text-primary" />
            <span>Interactive Demo Widget</span>
          </CardTitle>
          <CardDescription>
            Verify React state, interactivity, and design tokens work seamlessly in your components.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-4">
            <div className="rounded-xl border bg-muted/40 p-4 text-center min-w-[120px]">
              <p className="text-xs text-muted-foreground">State Counter</p>
              <p className="text-2xl font-bold text-foreground mt-1">{count}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button onClick={() => setCount((c) => c + 1)} size="sm">
                Increment (+1)
              </Button>
              <Button onClick={() => setCount((c) => Math.max(0, c - 1))} variant="outline" size="sm">
                Decrement (-1)
              </Button>
              <Button onClick={() => setCount(0)} variant="ghost" size="sm">
                Reset
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

export default ExamplePage;
