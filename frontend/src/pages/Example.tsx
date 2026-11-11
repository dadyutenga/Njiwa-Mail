import { useState } from "react";
import { ExampleForm } from "@/components/shared/ExampleForm";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Stepper, PrintableDocument, CopyButton } from "@/components/shared/Display";
export default function Example() {
  const [message, setMessage] = useState("");
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Example page</h1>
        <p className="mt-2 text-muted-foreground">
          Create a page component and add one entry to the route configuration to add it to the
          navigation.
        </p>
      </div>
      <section className="rounded-2xl border bg-card p-6">
        <h2 className="mb-5 text-xl font-semibold">Validated form</h2>
        <ExampleForm onSubmit={(values) => setMessage(`Saved locally: ${values.name}`)} />
        {message && (
          <p role="status" className="mt-4 text-primary">
            {message}
          </p>
        )}
      </section>
      <section className="rounded-2xl border bg-card p-6">
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="outline">Open form drawer</Button>
          </SheetTrigger>
          <SheetContent>
            <SheetTitle>Example form</SheetTitle>
            <SheetDescription className="mb-6">
              This form only updates the local example.
            </SheetDescription>
            <ExampleForm onSubmit={(values) => setMessage(`Saved locally: ${values.name}`)} />
          </SheetContent>
        </Sheet>
      </section>
      <section className="rounded-2xl border bg-card p-6">
        <h2 className="mb-4 font-semibold">Progress example</h2>
        <Stepper steps={["Start", "Review", "Finish"]} current={1} />
      </section>
      <PrintableDocument title="Example document">
        <p className="mb-4 text-muted-foreground">
          Replace this mock document with your own content.
        </p>
        <CopyButton text="Example document" />
      </PrintableDocument>
    </div>
  );
}
