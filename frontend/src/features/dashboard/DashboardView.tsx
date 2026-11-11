import { Link } from "react-router-dom";
import { AreaChartView } from "@/components/shared/Charts";
import { ChartCard, StatCard } from "@/components/shared/Display";
import { useI18n } from "@/features/i18n/i18n";
import { dashboardChartData, recentActivities } from "./mock-data";

export function DashboardView() {
  const { t } = useI18n();

  return (
    <div className="space-y-8">
      <div>
        <p className="text-sm font-semibold tracking-wider text-primary">{t.dashboard.tagline}</p>
        <h1 className="mt-2 text-3xl font-bold tracking-tight">{t.dashboard.title}</h1>
        <p className="mt-2 text-muted-foreground">{t.dashboard.subtitle}</p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label={t.dashboard.totalRecords} value="2,480" trend="+12% from last period" />
        <StatCard label={t.dashboard.activity} value="184" trend="+8% from last period" />
        <StatCard label={t.dashboard.completed} value="92%" trend="+4% from last period" />
        <StatCard label={t.dashboard.pending} value="24" trend="−2% from last period" />
      </div>

      <div className="grid gap-5 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <ChartCard title="Activity over time" description="Weekly throughput metrics">
            <AreaChartView data={dashboardChartData} />
          </ChartCard>
        </div>

        <section className="rounded-2xl border bg-card p-6">
          <h2 className="font-semibold">{t.dashboard.quickActions}</h2>
          <p className="mt-1 text-sm text-muted-foreground">Jump directly to common tasks</p>
          <div className="mt-6 space-y-3">
            {[
              ["/records", t.nav.records],
              ["/reports", t.nav.reports],
              ["/settings", t.nav.settings],
              ["/example", t.nav.example],
            ].map(([href, label]) => (
              <Link
                key={href}
                to={href}
                className="flex items-center justify-between rounded-xl border p-4 text-sm font-medium transition hover:bg-muted"
              >
                <span>{label}</span>
                <span className="text-muted-foreground">→</span>
              </Link>
            ))}
          </div>
        </section>
      </div>

      <section className="rounded-2xl border bg-card p-6">
        <h2 className="font-semibold">{t.dashboard.recentActivity}</h2>
        <div className="mt-4 overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b text-muted-foreground">
                <th scope="col" className="py-3 font-medium">
                  Activity
                </th>
                <th scope="col" className="py-3 font-medium">
                  When
                </th>
              </tr>
            </thead>
            <tbody>
              {recentActivities.map((item) => (
                <tr key={item.id} className="border-b last:border-0">
                  <td className="py-4 font-medium">{item.label}</td>
                  <td className="py-4 text-muted-foreground">{item.time}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
