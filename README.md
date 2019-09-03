This is a package for generating PDF files from Go.
The most interesting feature is its font support.

## Font Features

- Supports both TrueType and OpenType fonts.
- All text is UTF-8; supports any Unicode character (but only 255 per font).
- Embeds font subsets, converted to Type 3 outline fonts.

## Why Type 3?

PDF Type 3 fonts have a bad reputation, because they are often used for bitmap fonts.
But it's actually a pretty nice format.
The only real disadvantage is that it doesn't have hinting.
I like the fact that it's PDF objects all the way down—no binary blobs.
