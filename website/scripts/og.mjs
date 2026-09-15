// Generates public/og.png, the Open Graph image. The PNG is committed, so the
// build does not run this; run `pnpm og` after changing the copy or the logo.
//
// The image is an SVG rasterised by sharp (librsvg), so text uses system fonts
// on purpose: a generic sans and mono stack renders the same on macOS and on
// Ubuntu CI, where the site's web fonts are not installed.

import { fileURLToPath } from 'node:url';
import { stat } from 'node:fs/promises';
import path from 'node:path';
import sharp from 'sharp';

const here = path.dirname(fileURLToPath(import.meta.url));
const logoPath = path.resolve(here, '../../docs/assets/coding-owl-logo.png');
const outPath = path.resolve(here, '../public/og.png');

const W = 1200;
const H = 630;

// The site's palette (src/styles/global.css): neutral ground and ink, one sky
// accent, and a faint sky-300 grid behind the hero.
const ground = '#fafafa';
const ink = '#171717';
const inkDim = '#525252';
const inkFaint = '#737373';
const accent = '#0284c7';
const grid = '#7dd3fc';
const gridOpacity = 0.4;

const sans = "-apple-system, 'Helvetica Neue', Helvetica, Arial, sans-serif";
const mono = 'Menlo, Consolas, monospace';

// Copy, matching src/content/pages/home.md.
const eyebrow = 'Coding agents, while you are away';
const name = 'Coding Owl';
const tagline = 'Your machine works while you are away.';
const domain = 'codingowl.dev';

// Vertical rhythm, centred like the homepage hero.
const logoSize = 128;
const logoTop = 136;
const eyebrowY = 328;
const nameY = 410;
const taglineY = 470;

const escape = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

const svg = `
<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">
  <defs>
    <pattern id="grid" width="32" height="32" patternUnits="userSpaceOnUse">
      <path d="M 32 0 L 0 0 0 32" fill="none" stroke="${grid}" stroke-opacity="${gridOpacity}" stroke-width="1"/>
    </pattern>
    <radialGradient id="fade" cx="50%" cy="0%" r="80%" fx="50%" fy="0%">
      <stop offset="30%" stop-color="#fff" stop-opacity="1"/>
      <stop offset="100%" stop-color="#fff" stop-opacity="0"/>
    </radialGradient>
    <mask id="gridMask">
      <rect width="${W}" height="${H}" fill="url(#fade)"/>
    </mask>
  </defs>

  <rect width="${W}" height="${H}" fill="${ground}"/>
  <rect width="${W}" height="${H}" fill="url(#grid)" mask="url(#gridMask)"/>

  <text x="${W / 2}" y="${eyebrowY}" text-anchor="middle"
    font-family="${mono}" font-size="17" letter-spacing="2.4" fill="${inkFaint}">
    ${escape(eyebrow.toUpperCase())}
  </text>

  <text x="${W / 2}" y="${nameY}" text-anchor="middle"
    font-family="${sans}" font-size="76" font-weight="700" letter-spacing="-2" fill="${ink}">
    ${escape(name)}
  </text>

  <text x="${W / 2}" y="${taglineY}" text-anchor="middle"
    font-family="${sans}" font-size="32" font-weight="400" fill="${inkDim}">
    ${escape(tagline)}
  </text>

  <text x="${W - 56}" y="${H - 48}" text-anchor="end"
    font-family="${mono}" font-size="20" fill="${accent}">
    ${escape(domain)}
  </text>
</svg>`;

// The logo tile with rounded corners, ~22% radius like the Mark component.
const radius = Math.round(logoSize * 0.22);
const corners = Buffer.from(
  `<svg width="${logoSize}" height="${logoSize}"><rect width="${logoSize}" height="${logoSize}" rx="${radius}" ry="${radius}" fill="#fff"/></svg>`,
);
const logo = await sharp(logoPath)
  .resize(logoSize, logoSize)
  .composite([{ input: corners, blend: 'dest-in' }])
  .png()
  .toBuffer();

await sharp(Buffer.from(svg))
  .composite([{ input: logo, left: Math.round((W - logoSize) / 2), top: logoTop }])
  .png({ compressionLevel: 9, palette: true })
  .toFile(outPath);

const meta = await sharp(outPath).metadata();
const { size } = await stat(outPath);
console.log(`${path.relative(process.cwd(), outPath)}: ${meta.width}x${meta.height} ${meta.format}, ${size} bytes`);
