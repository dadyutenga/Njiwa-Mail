type BrandIconProps = {
  size?: number;
  className?: string;
};

export function BrandIcon({ size = 36, className }: BrandIconProps) {
  return (
    <img
      src="/njiwa-dove.svg"
      width={size}
      height={size}
      className={className}
      alt=""
      aria-hidden="true"
    />
  );
}
