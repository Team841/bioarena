"""Compose the practice-field splash: REBUILT over the two teams' logos.

Sources, all verified before use:
  - REBUILT 2026 wordmark: upstream cheesy-arena's static/img/game-logo.png. Pure black
    artwork on transparency, so it is reversed to white for the dark field palette --
    the standard treatment for a dark ground, not a recolour.
  - 8033 Highlander Robotics and 841 The BioMechs: each team's own GitHub organisation
    avatar, cross-checked against The Blue Alliance for the team name behind the number.
  - 6059 System Overload Robotics: their FRC avatar from The Blue Alliance. Their GitHub
    org has no uploaded avatar (GitHub serves a generated identicon) and frc6059.org does
    not resolve, so 40x40 is the best that exists publicly -- see restore_flat_art for
    what is done about that. Replace logos/tba6059.png with a real file and re-run.

The layout is laid out top-down against an explicit vertical budget and then asserted to
fit, because the first attempt overflowed 1080 and clipped both team tiles off the bottom
of the frame.
"""

import os
from PIL import Image, ImageChops, ImageDraw, ImageFilter, ImageFont

HERE = os.path.dirname(os.path.abspath(__file__))
LOGOS = os.path.join(HERE, "logos")
# Both the game logo read in and the splash written out live in the repo's static/img,
# which is what the audience display serves. Reading the game logo from there rather than
# keeping a second copy means a future game swaps in one place.
STATIC_IMG = os.path.join(HERE, "..", "..", "..", "..", "static", "img")
GAME_LOGO = os.path.join(STATIC_IMG, "game-logo.png")
OUT = os.path.join(STATIC_IMG, "oakland-splash.png")
FONTS = r"C:\Windows\Fonts"

W, H = 1920, 1080
BG = (11, 11, 13)
DIM = (150, 155, 165)
RULE = (40, 40, 48)

# Both marks already carry their team number, so the caption is the name alone.
TEAMS = [
    {"file": "gh8033.png", "name": "HIGHLANDER ROBOTICS", "accent": (124, 58, 190)},
    {"file": "gh841.png", "name": "THE BIOMECHS", "accent": (240, 196, 25)},
    # Only a 40x40 source exists, so this one is reconstructed rather than just scaled.
    {"file": "tba6059.png", "name": "SYSTEM OVERLOAD ROBOTICS", "accent": (186, 0, 0),
     "flat_art": True},
]

FOOTER = "10th St Robot Service Center  ·  Sponsored by KLA, PG&E, And More"

TOP_MARGIN = 100
WORDMARK_MAX_H = 330
GAP_WORDMARK_RULE = 74
GAP_RULE_TILES = 78
TILE = 268
GAP_TILE_CAPTION = 34
CAPTION_H = 34
FOOTER_FROM_BOTTOM = 76


def font(name, size):
    return ImageFont.truetype(os.path.join(FONTS, name), size)


def reversed_wordmark(path):
    """White version of black-on-transparent artwork, alpha untouched."""
    art = Image.open(path).convert("RGBA")
    white = Image.new("RGBA", art.size, (255, 255, 255, 0))
    white.putalpha(art.getchannel("A"))
    return white


def fit(image, box_w, box_h):
    """Scale to fit a box, preserving aspect. LANCZOS, since 841's mark is only 195px
    native and is the one thing here that gets enlarged."""
    scale = min(box_w / image.width, box_h / image.height)
    return image.resize((max(1, round(image.width * scale)),
                         max(1, round(image.height * scale))), Image.LANCZOS)


def rounded(image, radius):
    mask = Image.new("L", image.size, 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, image.width - 1, image.height - 1],
                                           radius=radius, fill=255)
    out = image.convert("RGBA")
    out.putalpha(mask)
    return out


def restore_flat_art(image, size):
    """Enlarge flat-colour artwork without the stair-stepping a plain upscale leaves.

    6059's mark exists publicly only as a 40x40 FRC avatar, so reaching tile size is a
    near sevenfold enlargement -- far enough that LANCZOS alone reads as a mistake beside
    the other two. The mark is three flat colours with no gradient worth preserving, so it
    is smoothed and then snapped back to exactly those colours, which puts the hard edges
    back. Still not a native-resolution file, and no substitute for one.
    """
    rgb = image.convert("RGB")
    palette = [c for _, c in sorted(rgb.getcolors(4096), key=lambda kv: -kv[0])[:3]]
    out = rgb.resize((size, size), Image.LANCZOS).filter(ImageFilter.GaussianBlur(1.4))
    px = out.load()
    for y in range(out.height):
        for x in range(out.width):
            r, g, b = px[x, y]
            px[x, y] = min(palette, key=lambda c: (r - c[0]) ** 2 + (g - c[1]) ** 2 + (b - c[2]) ** 2)
    return out.convert("RGBA")


def trim_to_ink(image):
    """Crop a logo to its artwork.

    The two marks arrive with different amounts of built-in whitespace -- 8033's is
    generously padded, 841's runs to its edges -- so sized as supplied one looks small
    beside the other. Trimming both to their ink and padding them equally below makes
    them optically the same size rather than nominally so.
    """
    rgb = image.convert("RGB")
    white = Image.new("RGB", rgb.size, (255, 255, 255))
    bbox = ImageChops.difference(rgb, white).getbbox()
    return image.crop(bbox) if bbox else image


def draw_centred(draw, text, cx, y, fnt, fill):
    box = draw.textbbox((0, 0), text, font=fnt)
    draw.text((cx - box[2] / 2, y), text, font=fnt, fill=fill)


canvas = Image.new("RGBA", (W, H), BG + (255,))
draw = ImageDraw.Draw(canvas)

# --- The game, across the top ------------------------------------------------
wordmark = fit(reversed_wordmark(GAME_LOGO),
               1120, WORDMARK_MAX_H)
canvas.alpha_composite(wordmark, ((W - wordmark.width) // 2, TOP_MARGIN))

rule_y = TOP_MARGIN + wordmark.height + GAP_WORDMARK_RULE
draw.line([(W // 2 - 400, rule_y), (W // 2 + 400, rule_y)], fill=RULE, width=3)

# --- The two teams, side by side ---------------------------------------------
GAP = 168
# n tiles have n-1 gaps between them, not one. Written as `+ GAP` this was right by
# accident for two teams and threw the whole group GAP/2 off centre at three.
total = len(TEAMS) * TILE + (len(TEAMS) - 1) * GAP
left = (W - total) // 2
tiles_top = rule_y + GAP_RULE_TILES

# Sized so the longest caption clears its neighbour; "SYSTEM OVERLOAD ROBOTICS" is
# considerably wider than its tile.
name_font = font("segoeuib.ttf", 25)

for i, team in enumerate(TEAMS):
    raw = Image.open(os.path.join(LOGOS, team["file"])).convert("RGBA")
    if team.get("flat_art"):
        raw = restore_flat_art(raw, TILE)
    logo = trim_to_ink(raw)
    # Squared on white with an even margin, so both tiles are the same shape whatever the
    # source was, neither mark is cropped, and both sit in the same amount of air.
    side = round(max(logo.size) * 1.26)
    square = Image.new("RGBA", (side, side), (255, 255, 255, 255))
    square.alpha_composite(logo, ((side - logo.width) // 2, (side - logo.height) // 2))
    tile = rounded(fit(square, TILE, TILE), 26)

    x = left + i * (TILE + GAP)
    canvas.alpha_composite(tile, (x + (TILE - tile.width) // 2, tiles_top))

    # A short accent rule in the team's own colour, so each caption belongs to the mark
    # above it rather than floating in grey.
    cx = x + TILE // 2
    accent_y = tiles_top + TILE + 18
    draw.line([(cx - 44, accent_y), (cx + 44, accent_y)], fill=team["accent"], width=4)
    draw_centred(draw, team["name"], cx, tiles_top + TILE + GAP_TILE_CAPTION + 6,
                 name_font, DIM)

captions_bottom = tiles_top + TILE + GAP_TILE_CAPTION + 6 + CAPTION_H

# --- Footer -------------------------------------------------------------------
footer_font = font("segoeuib.ttf", 25)
footer_y = H - FOOTER_FROM_BOTTOM
draw_centred(draw, FOOTER, W // 2, footer_y, footer_font, (122, 127, 138))

# The first attempt ran off the bottom of the frame; this is what stops that returning.
assert captions_bottom < footer_y - 20, (
    f"layout overflows: captions end at {captions_bottom}, footer starts at {footer_y}"
)

out = os.path.abspath(OUT)
canvas.convert("RGB").save(out)
print(f"wrote {out} {canvas.size}; captions end {captions_bottom}, footer {footer_y}")
