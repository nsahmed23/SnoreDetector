"""Generate a placeholder launch image at 1284×2778."""

from PIL import Image
import os


W, H = 1284, 2778
TOP = (10, 14, 30)
BOTTOM = (24, 32, 80)


def main() -> None:
    img = Image.new("RGBA", (W, H), TOP + (255,))
    # Vertical gradient via line-fill.
    for y in range(H):
        t = y / H
        r = int(TOP[0] + (BOTTOM[0] - TOP[0]) * t)
        g = int(TOP[1] + (BOTTOM[1] - TOP[1]) * t)
        b = int(TOP[2] + (BOTTOM[2] - TOP[2]) * t)
        img.paste((r, g, b, 255), (0, y, W, y + 1))

    icon_path = os.path.join(os.path.dirname(__file__), "..", "icon", "icon-1024.png")
    if os.path.exists(icon_path):
        icon = Image.open(icon_path).convert("RGBA")
        icon.thumbnail((W // 3, W // 3))
        ix = (W - icon.width) // 2
        iy = (H - icon.height) // 2
        img.paste(icon, (ix, iy), icon)

    out_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "launch-1284x2778.png")
    img.save(out_path, "PNG")
    print(f"wrote {out_path}")


if __name__ == "__main__":
    main()
