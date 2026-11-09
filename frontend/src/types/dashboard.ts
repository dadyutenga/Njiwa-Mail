export interface StatItem {
  id: string;
  title: string;
  value: string | number;
  change: number;
  changeType: "increase" | "decrease" | "neutral";
  period: string;
  icon?: string;
  description?: string;
}

export interface ChartDataPoint {
  label: string;
  value: number;
  secondaryValue?: number;
  category?: string;
}

export interface ActivityItem {
  id: string;
  user: {
    name: string;
    avatarUrl?: string;
  };
  action: string;
  target: string;
  timestamp: string;
  type: "create" | "update" | "delete" | "login" | "export";
}

export interface KpiMetric {
  label: string;
  current: number;
  target: number;
  unit: string;
  percent: number;
}
