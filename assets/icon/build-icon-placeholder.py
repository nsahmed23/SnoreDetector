"""Generate a placeholder app icon at 1024×1024 PNG.

Real icon design is a Mac-side task. This placeholder exists so
xcodegen has a non-broken AppIcon.appiconset bundle to package, and
so reviewers can see the intended composition. Run:

    python3 build-icon-placeholder.py

Outputs `icon-1024.png` in the same directory.
"""

from PIL import Image, ImageDraw, ImageFont
import math
import os


SIZE = 1024
ACCENT = (77, 111, 239)        # #4D6FEF
ACCENT_DARK = (128, 166, 244)  # #80A6F4
BACKGROUND = (10, 12, 20)      # near-black for contrast


def main() -> None:
    img = Image.new("RGBA", (SIZE, SIZE), BACKGROUND + (255,))
    draw = ImageDraw.Draw(img)

    # Sound-wave silhouette (procedural sine across the lower half).
    wave_y0 = SIZE * 0.62
    amp = SIZE * 0.06
    points = []
    for x in range(0, SIZE, 2):
        t = x / SIZE * 4 * math.pi
        y = wave_y0 + math.sin(t) * amp
        points.append((x, y))
    draw.line(points, fill=ACCENT + (255,), width=int(SIZE * 0.012))

    # Two stacked "z" glyphs at upper-left, scaled.
    # Drawn as filled polygons (no font — keeps the asset typeface-free).
    def draw_z(cx, cy, w, h, color):
        x0, y0 = cx - w / 2, cy - h / 2
        thickness = h * 0.18
        draw.polygon([
            (x0, y0), (x0 + w, y0), (x0 + w, y0 + thickness),
            (x0 + thickness, y0 + h - thickness),
            (x0 + w, y0 + h - thickness), (x0 + w, y0 + h),
            (x0, y0 + h), (x0, y0 + h - thickness),
            (x0 + w - thickness, y0 + thickness),
            (x0, y0 + thickness),
        ], fill=color + (255,))

    draw_z(SIZE * 0.42, SIZE * 0.38, SIZE * 0.32, SIZE * 0.28, ACCENT)
    draw_z(SIZE * 0.62, SIZE * 0.22, SIZE * 0.20, SIZE * 0.18, ACCENT_DARK)

    out_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "icon-1024.png")
    img.save(out_path, "PNG")
    print(f"wrote {out_path}")


if __name__ == "__main__":
    main()
