export const formatNumber = (value: number, locale = "en-US") =>
  new Intl.NumberFormat(locale).format(value);
export const formatCurrency = (value: number, currency = "USD", locale = "en-US") =>
  new Intl.NumberFormat(locale, { style: "currency", currency }).format(value);
export const formatDate = (value: Date | string, locale = "en-US") =>
  new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
export const formatBytes = (value: number, locale = "en-US") => {
  if (!Number.isFinite(value) || value < 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index =
    value === 0
      ? 0
      : Math.max(0, Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1));
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: index ? 1 : 0 }).format(value / 1024 ** index)} ${units[index]}`;
};
