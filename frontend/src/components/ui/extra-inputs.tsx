import { useId, useState, type ComponentProps } from "react";
import { Eye, EyeOff } from "lucide-react";
import { Input } from "./input";
import { Button } from "./button";
import { Label } from "./label";
export function IconButton({
  label,
  children,
  ...props
}: ComponentProps<typeof Button> & { label: string }) {
  return (
    <Button {...props} size="icon" aria-label={label}>
      {children}
    </Button>
  );
}
export function PasswordInput(props: ComponentProps<typeof Input>) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="flex gap-2">
      <Input {...props} type={visible ? "text" : "password"} />
      <IconButton
        type="button"
        variant="outline"
        label={visible ? "Hide password" : "Show password"}
        onClick={() => setVisible(!visible)}
      >
        {visible ? <EyeOff /> : <Eye />}
      </IconButton>
    </div>
  );
}
export function DatePicker(props: Omit<ComponentProps<typeof Input>, "type">) {
  return <Input {...props} type="date" />;
}
export function DateRangePicker({
  start,
  end,
  onStartChange,
  onEndChange,
}: {
  start: string;
  end: string;
  onStartChange: (value: string) => void;
  onEndChange: (value: string) => void;
}) {
  const id = useId();
  return (
    <div className="flex flex-wrap gap-4">
      <div>
        <Label htmlFor={`${id}-start`}>From</Label>
        <DatePicker
          id={`${id}-start`}
          value={start}
          onChange={(e) => onStartChange(e.target.value)}
        />
      </div>
      <div>
        <Label htmlFor={`${id}-end`}>To</Label>
        <DatePicker id={`${id}-end`} value={end} onChange={(e) => onEndChange(e.target.value)} />
      </div>
    </div>
  );
}
export function MultiSelect({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { value: string; label: string }[];
  value: string[];
  onChange: (value: string[]) => void;
}) {
  const id = useId();
  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <select
        id={id}
        multiple
        className="block w-full rounded-md border bg-background p-2"
        value={value}
        onChange={(e) => onChange(Array.from(e.target.selectedOptions, (o) => o.value))}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </div>
  );
}
export function FileUpload({
  label,
  ...props
}: Omit<ComponentProps<typeof Input>, "type"> & { label: string }) {
  const id = useId();
  return (
    <div>
      <Label htmlFor={id}>{label}</Label>
      <Input {...props} id={id} type="file" />
    </div>
  );
}
