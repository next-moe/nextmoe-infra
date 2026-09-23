#!/usr/bin/env python3
"""Draws the launch avatar frames: a static PNG and an animated WebP each.

Geometry follows the frame contract: a square canvas 1.2x the avatar, the
avatar circle centred with radius = canvas / 2.4, everything outside it free
for decoration.
"""
import math
import os
import shutil
import subprocess
import sys

S = 384
C = S / 2
R = S / 2.4
FRAMES = 36
FPS = 18


def pol(r, deg):
    a = math.radians(deg)
    return C + r * math.cos(a), C + r * math.sin(a)


def ring(color, r=R + 5, w=9, edge="#ffffff", edge_w=3):
    return (
        f'<circle cx="{C}" cy="{C}" r="{r}" fill="none" stroke="{color}" stroke-width="{w}"/>'
        f'<circle cx="{C}" cy="{C}" r="{R - 0.5}" fill="none" stroke="{edge}" stroke-width="{edge_w}"/>'
    )


def blossom(x, y, s, rot, petal="#ffd1df", line="#d9577f", heart="#ffd66b"):
    parts = []
    for i in range(5):
        a = rot + i * 72
        parts.append(
            f'<g transform="translate({x},{y}) rotate({a}) scale({s})">'
            f'<path d="M0,0 C-11,-6 -13,-20 -6,-26 L0,-21 L6,-26 C13,-20 11,-6 0,0 Z" '
            f'fill="{petal}" stroke="{line}" stroke-width="{2.2 / s}" stroke-linejoin="round"/></g>'
        )
    parts.append(f'<circle cx="{x}" cy="{y}" r="{5 * s}" fill="{heart}" stroke="{line}" stroke-width="1.6"/>')
    return "".join(parts)


def petal(x, y, s, rot, fill="#ffc2d4", line="#d9577f"):
    return (
        f'<g transform="translate({x},{y}) rotate({rot}) scale({s})">'
        f'<path d="M0,0 C-7,-4 -8,-13 -4,-17 L0,-14 L4,-17 C8,-13 7,-4 0,0 Z" '
        f'fill="{fill}" stroke="{line}" stroke-width="{1.8 / s}" stroke-linejoin="round"/></g>'
    )


def sparkle(x, y, s, fill, line=None):
    stroke = f' stroke="{line}" stroke-width="{1.6 / max(s, 0.01)}"' if line else ""
    return (
        f'<g transform="translate({x},{y}) scale({s})">'
        f'<path d="M0,-14 C2,-4 4,-2 14,0 C4,2 2,4 0,14 C-2,4 -4,2 -14,0 C-4,-2 -2,-4 0,-14 Z" '
        f'fill="{fill}"{stroke} stroke-linejoin="round"/></g>'
    )


def heart_shape(x, y, s, rot, fill, line):
    return (
        f'<g transform="translate({x},{y}) rotate({rot}) scale({s})">'
        f'<path d="M0,9 C-12,0 -14,-8 -8,-12 C-4,-14 -1,-12 0,-9 C1,-12 4,-14 8,-12 C14,-8 12,0 0,9 Z" '
        f'fill="{fill}" stroke="{line}" stroke-width="{2 / s}" stroke-linejoin="round"/></g>'
    )


def sakura(t):
    sway = 5 * math.sin(2 * math.pi * t)
    out = [ring("#f7b6cb", w=10), f'<circle cx="{C}" cy="{C}" r="{R + 11}" fill="none" stroke="#d9577f" stroke-width="2.5"/>']
    for deg, size in ((222, 1.1), (246, 0.75), (318, 0.85), (38, 1.0), (60, 0.65)):
        x, y = pol(R + 2, deg)
        out.append(blossom(x, y, size, deg + sway))
    for i in range(4):
        u = (t + i / 4) % 1
        deg = -70 + 160 * u
        x, y = pol(R + 12 + 3 * math.sin(2 * math.pi * (u * 2 + i / 4)), deg)
        out.append(petal(x, y, 0.85, 360 * u * 1.5 + i * 90))
    return out


def starlight(t):
    out = [
        ring("#4b5bd6", w=9),
        f'<circle cx="{C}" cy="{C}" r="{R + 14}" fill="none" stroke="#9aa6ff" stroke-width="2.5" stroke-dasharray="2 9" stroke-linecap="round"/>',
    ]
    mx, my = pol(R + 6, 315)
    out.append(
        f'<g transform="translate({mx},{my}) rotate(-20)">'
        f'<path d="M-6,-19 A20,20 0 1,0 14,13 A15,15 0 1,1 -6,-19 Z" fill="#ffe27a" stroke="#3a47b8" stroke-width="2.5" stroke-linejoin="round"/></g>'
    )
    stars = ((285, 0.95), (345, 0.8), (25, 1.05), (135, 1.0), (160, 0.7), (205, 0.9), (240, 0.7))
    for i, (deg, size) in enumerate(stars):
        phase = (t + i / len(stars)) % 1
        k = 0.7 + 0.4 * (0.5 + 0.5 * math.cos(2 * math.pi * phase))
        x, y = pol(R + 13, deg)
        out.append(sparkle(x, y, size * k * 1.2, "#fff4b8", "#3a47b8"))
    deg = 360 * t - 90
    for j in range(5):
        x, y = pol(R + 5, deg - j * 5)
        out.append(f'<circle cx="{x}" cy="{y}" r="{4.5 - j * 0.8}" fill="#ffffff" opacity="{1 - j * 0.18}"/>')
    return out


def ear(deg, twitch, outer, inner, line):
    bx, by = pol(R - 16, deg)
    g = f'<g transform="translate({bx},{by}) rotate({deg + 90 + twitch}) scale(0.84)">'
    g += (
        f'<path d="M-30,6 C-24,-22 -10,-44 2,-52 C10,-40 22,-20 30,6 Z" fill="{outer}" stroke="{line}" '
        f'stroke-width="4.5" stroke-linejoin="round"/>'
        f'<path d="M-15,0 C-11,-17 -4,-32 2,-38 C7,-30 13,-16 16,0 Z" fill="{inner}"/>'
    )
    return g + "</g>"


def paw(x, y, s, fill, line):
    toes = "".join(
        f'<circle cx="{dx}" cy="{dy}" r="3.6" fill="{fill}" stroke="{line}" stroke-width="1.5"/>'
        for dx, dy in ((-7, -7), (-2.5, -11), (2.5, -11), (7, -7))
    )
    return (
        f'<g transform="translate({x},{y}) scale({s})">{toes}'
        f'<ellipse cx="0" cy="1" rx="7.5" ry="6" fill="{fill}" stroke="{line}" stroke-width="1.5"/></g>'
    )


def neko(t):
    out = [ring("#ffb86b", w=10), f'<circle cx="{C}" cy="{C}" r="{R + 11}" fill="none" stroke="#c9702b" stroke-width="2.5"/>']
    u = t * FRAMES
    twitch = -9 * math.sin(math.pi * (u - 8) / 5) if 8 <= u <= 13 else 0
    out.append(ear(238, 0, "#ffb86b", "#ffd6e2", "#c9702b"))
    out.append(ear(302, twitch, "#ffb86b", "#ffd6e2", "#c9702b"))
    bob = 3 * math.sin(2 * math.pi * t)
    for deg, s in ((72, 1.15), (98, 1.15)):
        x, y = pol(R + 10, deg)
        out.append(paw(x, y + (bob if deg == 72 else -bob), s, "#ffe8d2", "#c9702b"))
    return out


def laurel(t):
    out = [ring("#e5b93c", w=7, edge="#fff7da"), f'<circle cx="{C}" cy="{C}" r="{R + 10}" fill="none" stroke="#a87a12" stroke-width="2"/>']
    leaves = []
    for side in (1, -1):
        for k in range(8):
            deg = 90 + side * (18 + k * 15)
            size = 1 - k * 0.04
            for off in (-1, 1):
                lx, ly = pol(R + 9 + off * 8, deg)
                tilt = deg + side * 90 - off * side * 35
                leaves.append(
                    f'<ellipse cx="{lx}" cy="{ly}" rx="{6.5 * size}" ry="{13 * size}" '
                    f'transform="rotate({tilt},{lx},{ly})" fill="#6cbf6e" stroke="#2f7d3f" stroke-width="2"/>'
                )
    out.extend(leaves)
    bx, by = pol(R + 4, 90)
    out.append(
        f'<g transform="translate({bx},{by})">'
        f'<path d="M-4,0 L-14,18 L-8,16 L-5,22 L0,6 Z" fill="#e0445a" stroke="#9c2436" stroke-width="2" stroke-linejoin="round"/>'
        f'<path d="M4,0 L14,18 L8,16 L5,22 L0,6 Z" fill="#e0445a" stroke="#9c2436" stroke-width="2" stroke-linejoin="round"/>'
        f'<circle r="8" fill="#ff6b80" stroke="#9c2436" stroke-width="2"/></g>'
    )
    deg = -90 + 360 * t
    x, y = pol(R + 5, deg)
    out.append(sparkle(x, y, 0.9, "#ffffff", "#a87a12"))
    return out


def moe_heart(t):
    out = [ring("#ff7aa8", w=9), f'<circle cx="{C}" cy="{C}" r="{R + 11}" fill="none" stroke="#c83f72" stroke-width="2.5"/>']
    for i in range(6):
        deg = -90 + i * 60 + 60 * t
        pulse = 1 + 0.12 * math.sin(2 * math.pi * (t * 2 + i / 6))
        x, y = pol(R + 9, deg)
        fill = "#ff9cc2" if i % 2 else "#ffffff"
        out.append(heart_shape(x, y, 1.45 * pulse, deg + 90, fill, "#c83f72"))
    return out


DESIGNS = {
    "sakura": sakura,
    "starlight": starlight,
    "neko": neko,
    "laurel": laurel,
    "moe-heart": moe_heart,
}


def svg(elements):
    body = "".join(elements)
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{S}" height="{S}" viewBox="0 0 {S} {S}">{body}</svg>'


def render(out_dir, name, draw):
    work = os.path.join(out_dir, f".{name}")
    shutil.rmtree(work, ignore_errors=True)
    os.makedirs(work)
    for f in range(FRAMES):
        src = os.path.join(work, f"{f:03d}.svg")
        with open(src, "w") as fh:
            fh.write(svg(draw(f / FRAMES)))
        subprocess.run(["rsvg-convert", "-o", os.path.join(work, f"{f:03d}.png"), src], check=True)
    for f in range(FRAMES):
        edge = subprocess.run(
            ["magick", os.path.join(work, f"{f:03d}.png"), "-alpha", "extract",
             "(", "+clone", "-shave", "1x1", "-fill", "black", "-colorize", "100", "-bordercolor", "white", "-border", "1", ")",
             "-compose", "multiply", "-composite", "-format", "%[fx:maxima]", "info:"],
            capture_output=True, text=True, check=True,
        ).stdout
        if float(edge or 0) > 0:
            raise SystemExit(f"{name} frame {f}: decoration touches the canvas edge")
    shutil.copy(os.path.join(work, "000.png"), os.path.join(out_dir, f"{name}.png"))
    subprocess.run(
        ["ffmpeg", "-loglevel", "error", "-y", "-framerate", str(FPS), "-i", os.path.join(work, "%03d.png"),
         "-c:v", "libwebp_anim", "-lossless", "0", "-quality", "70", "-compression_level", "6", "-loop", "0", "-pix_fmt", "yuva420p",
         os.path.join(out_dir, f"{name}.webp")],
        check=True,
    )
    shutil.rmtree(work)


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else "out"
    os.makedirs(out, exist_ok=True)
    for name, draw in DESIGNS.items():
        render(out, name, draw)
        print(name, os.path.getsize(os.path.join(out, f"{name}.png")), os.path.getsize(os.path.join(out, f"{name}.webp")))
