# Njiwa-Mail welcome page design

The page resembles a piece of airmail: a warm paper canvas, an editorial serif headline, a blue route line, and a single red postmark. The dark theme uses a night sky and ink-like panels. The code card anchors the hero; later sections alternate between open paper and framed surfaces.

## Type and spacing

- Display: Georgia, serif. Body: system UI sans. Code: system UI monospace. These local fonts need no network fetch or font swap and avoid layout shift.
- Type scale: 12, 14, 16, 18, 24, 32, 48, 72 px, with fluid hero sizing between 48 and 88 px.
- Spacing scale: 4, 8, 12, 16, 24, 32, 48, 64, 96 px. Sections use 96 px on desktop and 64 px on small screens.

## Colour

| Token | Light | Dark |
|---|---|---|
| Background | `#FAF7F2` | `#0F1114` |
| Surface | `#FFFFFF` | `#171A1F` |
| Text | `#1B1A17` | `#ECE8E1` |
| Muted text | `#6B675F` | `#9A968E` |
| Line | `#E4DED3` | `#2A2E35` |
| Blue accent | `#2F5BEA` | `#7C9BFF` |
| Red mark | `#D9432F` | `#FF7A66` |
| Code background | `#14161A` | `#0A0C0F` |

WCAG contrast ratios: light muted text on background 5.27:1; dark muted text on background 6.42:1; light blue on white 5.52:1; dark blue on surface 6.64:1; white on light blue 5.52:1; dark background on dark blue 7.19:1. All exceed 4.5:1 for normal text.

The red and blue airmail stripe appears at the top and on the code panel. The original dove SVG serves as the brand mark and favicon. A small circular postmark sits behind the hero copy. Red is decorative except for error states.
