import * as React from "react";
import { UploadCloud, File, X, CheckCircle2 } from "lucide-react";
import { cn, formatBytes } from "@/lib/utils";
import { Button } from "@/components/ui/button";

export interface FileUploadProps {
  onFilesSelected?: (files: File[]) => void;
  maxFiles?: number;
  maxSizeMB?: number;
  accept?: string;
  className?: string;
}

export function FileUpload({
  onFilesSelected,
  maxFiles = 5,
  maxSizeMB = 10,
  accept = "*/*",
  className,
}: FileUploadProps) {
  const [files, setFiles] = React.useState<File[]>([]);
  const [isDragOver, setIsDragOver] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const inputRef = React.useRef<HTMLInputElement>(null);

  const handleFiles = (incomingFiles: FileList | null) => {
    if (!incomingFiles) return;
    setError(null);

    const validFiles: File[] = [];
    const maxSizeBytes = maxSizeMB * 1024 * 1024;

    for (let i = 0; i < incomingFiles.length; i++) {
      const file = incomingFiles[i];
      if (file.size > maxSizeBytes) {
        setError(`File "${file.name}" exceeds ${maxSizeMB}MB limit.`);
        continue;
      }
      validFiles.push(file);
    }

    const updated = [...files, ...validFiles].slice(0, maxFiles);
    setFiles(updated);
    onFilesSelected?.(updated);
  };

  const removeFile = (index: number) => {
    const updated = files.filter((_, i) => i !== index);
    setFiles(updated);
    onFilesSelected?.(updated);
  };

  return (
    <div className={cn("w-full space-y-3", className)}>
      <div
        onDragOver={(e) => {
          e.preventDefault();
          setIsDragOver(true);
        }}
        onDragLeave={() => setIsDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setIsDragOver(false);
          handleFiles(e.dataTransfer.files);
        }}
        onClick={() => inputRef.current?.click()}
        className={cn(
          "flex flex-col items-center justify-center border-2 border-dashed rounded-xl p-6 cursor-pointer transition-colors text-center",
          isDragOver
            ? "border-primary bg-primary/5"
            : "border-border hover:border-primary/50 hover:bg-muted/30",
        )}
      >
        <input
          ref={inputRef}
          type="file"
          multiple={maxFiles > 1}
          accept={accept}
          onChange={(e) => handleFiles(e.target.files)}
          className="hidden"
        />
        <div className="p-3 rounded-full bg-primary/10 text-primary mb-3">
          <UploadCloud className="h-6 w-6" />
        </div>
        <p className="text-sm font-semibold text-foreground">
          Click to upload or drag and drop
        </p>
        <p className="text-xs text-muted-foreground mt-1">
          Max {maxFiles} files up to {maxSizeMB}MB each
        </p>
      </div>

      {error && <p className="text-xs text-destructive">{error}</p>}

      {files.length > 0 && (
        <ul className="space-y-2 mt-2">
          {files.map((file, idx) => (
            <li
              key={`${file.name}-${idx}`}
              className="flex items-center justify-between p-2.5 rounded-lg border bg-card text-card-foreground text-sm"
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <File className="h-4 w-4 shrink-0 text-muted-foreground" />
                <span className="truncate font-medium">{file.name}</span>
                <span className="text-xs text-muted-foreground shrink-0">
                  ({formatBytes(file.size)})
                </span>
              </div>
              <div className="flex items-center gap-1 shrink-0 ml-2">
                <CheckCircle2 className="h-4 w-4 text-emerald-500" />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6 text-muted-foreground hover:text-destructive"
                  onClick={(e) => {
                    e.stopPropagation();
                    removeFile(idx);
                  }}
                  aria-label={`Remove ${file.name}`}
                >
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
