import * as React from "react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  BarChart,
  Bar,
  AreaChart,
  Area,
  PieChart,
  Pie,
  Cell,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from "recharts";

const THEME_COLORS = [
  "hsl(187 85% 43%)", // Primary cyan/teal
  "hsl(25 95% 65%)",  // Secondary orange
  "hsl(142 70% 55%)", // Accent green
  "hsl(280 65% 60%)", // Purple
  "hsl(340 75% 55%)", // Pink
  "hsl(45 90% 50%)",  // Amber
];

// Area Chart Wrapper
export interface AreaChartDataPoint {
  name: string;
  [key: string]: string | number;
}

export interface AreaChartProps {
  data: AreaChartDataPoint[];
  categories: { key: string; name: string; color?: string }[];
  indexKey?: string;
  height?: number;
}

export function AreaChartWrapper({
  data,
  categories,
  indexKey = "name",
  height = 300,
}: AreaChartProps) {
  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
          <defs>
            {categories.map((cat, idx) => {
              const color = cat.color || THEME_COLORS[idx % THEME_COLORS.length];
              return (
                <linearGradient key={cat.key} id={`gradient-${cat.key}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={color} stopOpacity={0.4} />
                  <stop offset="95%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              );
            })}
          </defs>
          <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.2} />
          <XAxis dataKey={indexKey} tickLine={false} axisLine={false} tick={{ fontSize: 12 }} />
          <YAxis tickLine={false} axisLine={false} tick={{ fontSize: 12 }} />
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(var(--card))",
              borderColor: "hsl(var(--border))",
              borderRadius: "0.5rem",
              fontSize: "12px",
            }}
          />
          <Legend wrapperStyle={{ fontSize: "12px", paddingTop: "10px" }} />
          {categories.map((cat, idx) => {
            const color = cat.color || THEME_COLORS[idx % THEME_COLORS.length];
            return (
              <Area
                key={cat.key}
                type="monotone"
                dataKey={cat.key}
                name={cat.name}
                stroke={color}
                fillOpacity={1}
                fill={`url(#gradient-${cat.key})`}
                strokeWidth={2}
              />
            );
          })}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

// Line Chart Wrapper
export interface LineChartProps {
  data: AreaChartDataPoint[];
  lines: { key: string; name: string; color?: string }[];
  indexKey?: string;
  height?: number;
}

export function LineChartWrapper({
  data,
  lines,
  indexKey = "name",
  height = 300,
}: LineChartProps) {
  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.2} />
          <XAxis dataKey={indexKey} tickLine={false} axisLine={false} tick={{ fontSize: 12 }} />
          <YAxis tickLine={false} axisLine={false} tick={{ fontSize: 12 }} />
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(var(--card))",
              borderColor: "hsl(var(--border))",
              borderRadius: "0.5rem",
              fontSize: "12px",
            }}
          />
          <Legend wrapperStyle={{ fontSize: "12px", paddingTop: "10px" }} />
          {lines.map((line, idx) => {
            const color = line.color || THEME_COLORS[idx % THEME_COLORS.length];
            return (
              <Line
                key={line.key}
                type="monotone"
                dataKey={line.key}
                name={line.name}
                stroke={color}
                strokeWidth={2}
                dot={{ r: 3 }}
                activeDot={{ r: 5 }}
              />
            );
          })}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

// Bar Chart Wrapper
export interface BarChartProps {
  data: AreaChartDataPoint[];
  bars: { key: string; name: string; color?: string }[];
  indexKey?: string;
  height?: number;
}

export function BarChartWrapper({
  data,
  bars,
  indexKey = "name",
  height = 300,
}: BarChartProps) {
  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={data} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.2} />
          <XAxis dataKey={indexKey} tickLine={false} axisLine={false} tick={{ fontSize: 12 }} />
          <YAxis tickLine={false} axisLine={false} tick={{ fontSize: 12 }} />
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(var(--card))",
              borderColor: "hsl(var(--border))",
              borderRadius: "0.5rem",
              fontSize: "12px",
            }}
          />
          <Legend wrapperStyle={{ fontSize: "12px", paddingTop: "10px" }} />
          {bars.map((bar, idx) => {
            const color = bar.color || THEME_COLORS[idx % THEME_COLORS.length];
            return (
              <Bar
                key={bar.key}
                dataKey={bar.key}
                name={bar.name}
                fill={color}
                radius={[4, 4, 0, 0]}
              />
            );
          })}
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

// Pie Chart Wrapper
export interface PieChartDataPoint {
  name: string;
  value: number;
  color?: string;
}

export interface PieChartProps {
  data: PieChartDataPoint[];
  height?: number;
  innerRadius?: number;
  outerRadius?: number;
}

export function PieChartWrapper({
  data,
  height = 300,
  innerRadius = 60,
  outerRadius = 90,
}: PieChartProps) {
  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Tooltip
            contentStyle={{
              backgroundColor: "hsl(var(--card))",
              borderColor: "hsl(var(--border))",
              borderRadius: "0.5rem",
              fontSize: "12px",
            }}
          />
          <Legend wrapperStyle={{ fontSize: "12px" }} />
          <Pie
            data={data}
            cx="50%"
            cy="50%"
            innerRadius={innerRadius}
            outerRadius={outerRadius}
            paddingAngle={3}
            dataKey="value"
          >
            {data.map((entry, index) => (
              <Cell
                key={`cell-${entry.name}-${index}`}
                fill={entry.color || THEME_COLORS[index % THEME_COLORS.length]}
              />
            ))}
          </Pie>
        </PieChart>
      </ResponsiveContainer>
    </div>
  );
}
