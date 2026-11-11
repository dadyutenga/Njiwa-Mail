import { useState } from "react";
import { BarChartView, LineChartView, PieChartView } from "@/components/shared/Charts";
import { ChartCard, EmptyState } from "@/components/shared/Display";
import { DateRangePicker } from "@/components/ui/extra-inputs";
import { Button } from "@/components/ui/button";
import { downloadCsv } from "@/lib/csv";
import { useI18n } from "@/features/i18n/i18n";
import { initialReportData } from "./mock-data";

export function ReportsView() {
  const [start, setStart] = useState("2026-01-01");
  const [end, setEnd] = useState("2026-06-30");
  const { t } = useI18n();

  const invalid = Boolean(start && end && start > end);
  const filtered = invalid
    ? []
    : initialReportData.filter((row) => (!start || row.date >= start) && (!end || row.date <= end));

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t.nav.reports}</h1>
        <p className="mt-2 text-muted-foreground">Explore mock analytics for your workspace.</p>
      </div>

      <div className="flex flex-wrap items-end justify-between gap-4 rounded-2xl border bg-card p-5">
        <DateRangePicker start={start} end={end} onStartChange={setStart} onEndChange={setEnd} />
        <Button
          variant="outline"
          disabled={!filtered.length}
          onClick={() =>
            downloadCsv("report.csv", [
              ["Date", "Period", "Value"],
              ...filtered.map(({ date, name, value }) => [date, name, value]),
            ])
          }
        >
          {t.actions.exportCsv}
        </Button>
        {invalid && (
          <p role="alert" className="w-full text-sm text-destructive">
            The start date must be before the end date.
          </p>
        )}
      </div>

      {filtered.length ? (
        <div className="grid gap-5 lg:grid-cols-2">
          <ChartCard title="Activity by period">
            <BarChartView data={filtered} />
          </ChartCard>
          <ChartCard title="Trend">
            <LineChartView data={filtered} />
          </ChartCard>
          <ChartCard title="Distribution">
            <PieChartView data={filtered} />
          </ChartCard>
        </div>
      ) : (
        <EmptyState title="No data in this range" />
      )}
    </div>
  );
}
