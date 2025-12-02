const hslVar = (token: string, alpha?: number) => (alpha !== undefined ? `hsl(var(${token}) / ${alpha})` : `hsl(var(${token}))`);

export const themeColors = {
  background: hslVar("--background"),
  foreground: hslVar("--foreground"),
  card: hslVar("--card"),
  cardMuted: hslVar("--card", 0.85),
  border: hslVar("--border"),
  borderMuted: hslVar("--border", 0.4),
  mutedForeground: hslVar("--muted-foreground"),
  primary: hslVar("--primary"),
  secondary: hslVar("--secondary"),
  positive: hslVar("--positive"),
  negative: hslVar("--negative"),
  warning: hslVar("--warning"),
  info: hslVar("--info"),
};

export const chartPalettes = {
  mixed: [
    hslVar("--chart-mixed-1"),
    hslVar("--chart-mixed-2"),
    hslVar("--chart-mixed-3"),
    hslVar("--chart-mixed-4"),
    hslVar("--chart-mixed-5"),
    hslVar("--chart-mixed-6"),
    hslVar("--chart-mixed-7"),
    hslVar("--chart-mixed-8"),
    hslVar("--chart-mixed-9"),
  ],
  warm: [
    hslVar("--chart-warm-1"),
    hslVar("--chart-warm-2"),
    hslVar("--chart-warm-3"),
    hslVar("--chart-warm-4"),
    hslVar("--chart-warm-5"),
    hslVar("--chart-warm-6"),
    hslVar("--chart-warm-7"),
    hslVar("--chart-warm-8"),
  ],
  cool: [
    hslVar("--chart-cool-1"),
    hslVar("--chart-cool-2"),
    hslVar("--chart-cool-3"),
    hslVar("--chart-cool-4"),
    hslVar("--chart-cool-5"),
    hslVar("--chart-cool-6"),
    hslVar("--chart-cool-7"),
    hslVar("--chart-cool-8"),
  ],
};

export { hslVar };

