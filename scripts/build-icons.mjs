// FloatTranslate icon builder — single source of truth: the 16x16 pixel
// matrix below (blue/white pixel-art brand logo).
//
// Outputs (run: node scripts/build-icons.mjs):
//   logo.svg                        repo-root brand SVG (for README)
//   src-tauri/icons/icon.ico        16/32/48/64 (BMP) + 128/256 (PNG) entries
//   src-tauri/icons/32x32.png
//   src-tauri/icons/128x128.png
//   src-tauri/icons/128x128@2x.png  (256px)
//
// Every raster size is an exact integer multiple of the 16px grid, so the
// pixel art stays crisp from tray (16) to installer (256). Dependency-free:
// PNG is hand-encoded via node:zlib, ICO entries are packed directly.

import { deflateSync } from 'node:zlib';
import { writeFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');

// ---------------------------------------------------------------------------
// 1. Design: 16x16 pixel matrix built from drawing primitives (blue/white
//    pixel-art: navy-outlined blue badge + white speech bubble + blue "A").
//    '.' = transparent; one char per palette slot.
// ---------------------------------------------------------------------------

const PALETTE = {
  N: '#1E3A8A', // navy outline (blue-900)
  B: '#2563EB', // primary blue (blue-600)
  W: '#FFFFFF', // white
};

const GRID = 16;
const cells = Array.from({ length: GRID }, () => Array(GRID).fill('.'));
const put = (x, y, ch) => {
  cells[y][x] = ch;
};
// Filled rect.
const fill = (x0, y0, x1, y1, ch) => {
  for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) put(x, y, ch);
};
// 1px outline rect.
const ring = (x0, y0, x1, y1, ch) => {
  fill(x0, y0, x1, y0, ch); fill(x0, y1, x1, y1, ch);
  fill(x0, y0, x0, y1, ch); fill(x1, y0, x1, y1, ch);
};

// Badge: rounded square (2-step diagonal corners), navy ring, blue fill.
fill(0, 2, 15, 13, 'N');
fill(2, 0, 13, 15, 'N');
put(1, 1, 'N'); put(14, 1, 'N'); put(1, 14, 'N'); put(14, 14, 'N');
fill(1, 2, 14, 13, 'B');
fill(2, 1, 13, 14, 'B');

// Speech bubble: white with navy outline, tail at bottom-left.
ring(3, 3, 12, 10, 'N');
fill(4, 4, 11, 9, 'W');
put(5, 11, 'W'); put(6, 11, 'W'); put(5, 12, 'W');

// Letter "A" (4x4, blue) centered in the bubble interior (x4..11).
put(7, 5, 'B'); put(8, 5, 'B');
put(6, 6, 'B'); put(9, 6, 'B');
fill(6, 7, 9, 7, 'B');
put(6, 8, 'B'); put(9, 8, 'B');

const MATRIX = cells.map((row) => row.join(''));

// ---------------------------------------------------------------------------
// 2. SVG (canonical brand asset): horizontal run-length merged rects.
// ---------------------------------------------------------------------------

function svgFromMatrix(matrix) {
  const h = matrix.length;
  const w = matrix[0].length;
  const rects = [];
  for (let y = 0; y < h; y++) {
    let x = 0;
    while (x < w) {
      const ch = matrix[y][x];
      if (ch === '.') { x++; continue; }
      let run = 1;
      while (x + run < w && matrix[y][x + run] === ch) run++;
      rects.push(
        `    <rect x="${x}" y="${y}" width="${run}" height="1" fill="${PALETTE[ch]}"/>`,
      );
      x += run;
    }
  }
  return [
    '<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256"',
    '     viewBox="0 0 16 16" shape-rendering="crispEdges">',
    `  <title>FloatTranslate</title>`,
    ...rects,
    '</svg>',
    '',
  ].join('\n');
}

// ---------------------------------------------------------------------------
// 3. Raster: scale matrix by integer k into RGBA buffer.
// ---------------------------------------------------------------------------

function hexToRgba(hex) {
  const v = parseInt(hex.slice(1), 16);
  return [(v >>> 16) & 255, (v >>> 8) & 255, v & 255, 255];
}

function rasterize(matrix, k) {
  const h = matrix.length;
  const w = matrix[0].length;
  const out = Buffer.alloc(w * k * h * k * 4);
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      const ch = matrix[y][x];
      if (ch === '.') continue;
      const [r, g, b, a] = hexToRgba(PALETTE[ch]);
      for (let dy = 0; dy < k; dy++) {
        const row = (y * k + dy) * w * k;
        for (let dx = 0; dx < k; dx++) {
          const i = (row + x * k + dx) * 4;
          out[i] = r; out[i + 1] = g; out[i + 2] = b; out[i + 3] = a;
        }
      }
    }
  }
  return { width: w * k, height: h * k, rgba: out };
}

// ---------------------------------------------------------------------------
// 4. Minimal PNG encoder (8-bit RGBA, filter 0, non-interlaced).
// ---------------------------------------------------------------------------

const CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let i = 0; i < 8; i++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

function crc32(buf) {
  let c = 0xffffffff;
  for (const byte of buf) c = CRC_TABLE[(c ^ byte) & 255] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function pngChunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
}

function encodePng({ width, height, rgba }) {
  const stride = width * 4;
  const raw = Buffer.alloc((stride + 1) * height);
  for (let y = 0; y < height; y++) {
    raw[y * (stride + 1)] = 0; // filter: none
    rgba.copy(raw, y * (stride + 1) + 1, y * stride, (y + 1) * stride);
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = 8;  // bit depth
  ihdr[9] = 6;  // color type: RGBA
  // bytes 10-12: compression, filter, interlace = 0
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', deflateSync(raw, { level: 9 })),
    pngChunk('IEND', Buffer.alloc(0)),
  ]);
}

// ---------------------------------------------------------------------------
// 5. ICO packer: BMP (DIB) entries for <=64px, PNG entries for >=128px.
// ---------------------------------------------------------------------------

function bmpEntry({ width, height, rgba }) {
  const header = Buffer.alloc(40);
  header.writeUInt32LE(40, 0);
  header.writeInt32LE(width, 4);
  header.writeInt32LE(height * 2, 8); // XOR + AND
  header.writeUInt16LE(1, 12);        // planes
  header.writeUInt16LE(32, 14);       // bpp
  header.writeUInt32LE(0, 16);        // BI_RGB

  const xor = Buffer.alloc(width * height * 4);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const si = (y * width + x) * 4;
      const di = ((height - 1 - y) * width + x) * 4; // bottom-up
      xor[di] = rgba[si + 2];     // B
      xor[di + 1] = rgba[si + 1]; // G
      xor[di + 2] = rgba[si];     // R
      xor[di + 3] = rgba[si + 3]; // A
    }
  }

  const maskStride = Math.ceil(width / 32) * 4;
  const and = Buffer.alloc(maskStride * height); // fully transparent-aware alpha; mask bits = 0 (opaque check via alpha)
  return Buffer.concat([header, xor, and]);
}

function buildIco(images) {
  const entries = images.map((img) => ({
    size: img.width,
    data: img.width <= 64 ? bmpEntry(img) : encodePng(img),
  }));
  const header = Buffer.alloc(6);
  header.writeUInt16LE(0, 0);
  header.writeUInt16LE(1, 2); // type: icon
  header.writeUInt16LE(entries.length, 4);

  const dir = Buffer.alloc(16 * entries.length);
  let offset = 6 + 16 * entries.length;
  entries.forEach((e, i) => {
    const o = i * 16;
    dir[o] = e.size >= 256 ? 0 : e.size;
    dir[o + 1] = e.size >= 256 ? 0 : e.size;
    dir[o + 2] = 0; // colors
    dir[o + 3] = 0; // reserved
    dir.writeUInt16LE(1, o + 4);           // planes
    dir.writeUInt16LE(32, o + 6);          // bpp
    dir.writeUInt32LE(e.data.length, o + 8);
    dir.writeUInt32LE(offset, o + 12);
    offset += e.data.length;
  });

  return Buffer.concat([header, dir, ...entries.map((e) => e.data)]);
}

// ---------------------------------------------------------------------------
// 6. Emit everything.
// ---------------------------------------------------------------------------

const grid = MATRIX[0].length;
if (MATRIX.some((row) => row.length !== grid)) {
  throw new Error('pixel matrix rows must all be the same width');
}
const size = (k) => rasterize(MATRIX, k);

mkdirSync(join(ROOT, 'src-tauri', 'icons'), { recursive: true });

writeFileSync(join(ROOT, 'logo.svg'), svgFromMatrix(MATRIX));

const ico = buildIco([size(1), size(2), size(3), size(4), size(8), size(16)]);
writeFileSync(join(ROOT, 'src-tauri', 'icons', 'icon.ico'), ico);

writeFileSync(join(ROOT, 'src-tauri', 'icons', '32x32.png'), encodePng(size(2)));
writeFileSync(join(ROOT, 'src-tauri', 'icons', '128x128.png'), encodePng(size(8)));
writeFileSync(join(ROOT, 'src-tauri', 'icons', '128x128@2x.png'), encodePng(size(16)));

// ---------------------------------------------------------------------------
// 5b. BMP encoder (24bpp, bottom-up) for NSIS wizard images.
// ---------------------------------------------------------------------------

function encodeBmp({ width, height, rgba, bg }) {
  const [br, bgc, bb] = bg;
  const rowStride = Math.ceil((width * 3) / 4) * 4;
  const pixels = Buffer.alloc(rowStride * height);
  for (let y = 0; y < height; y++) {
    const outRow = (height - 1 - y) * rowStride; // bottom-up
    for (let x = 0; x < width; x++) {
      const si = (y * width + x) * 4;
      const di = outRow + x * 3;
      const a = rgba[si + 3] / 255;
      // alpha-blend over the solid background (BMP has no alpha)
      pixels[di] = Math.round(rgba[si + 2] * a + bb * (1 - a));
      pixels[di + 1] = Math.round(rgba[si + 1] * a + bgc * (1 - a));
      pixels[di + 2] = Math.round(rgba[si] * a + br * (1 - a));
    }
  }
  const fileHeader = Buffer.alloc(14);
  const infoHeader = Buffer.alloc(40);
  const fileSize = 54 + pixels.length;
  fileHeader.write('BM', 0, 'ascii');
  fileHeader.writeUInt32LE(fileSize, 2);
  fileHeader.writeUInt32LE(54, 10);
  infoHeader.writeUInt32LE(40, 0);
  infoHeader.writeInt32LE(width, 4);
  infoHeader.writeInt32LE(height, 8);
  infoHeader.writeUInt16LE(1, 12);
  infoHeader.writeUInt16LE(24, 14);
  infoHeader.writeUInt32LE(pixels.length, 20);
  return Buffer.concat([fileHeader, infoHeader, pixels]);
}

// NSIS wizard art: header 150x57, sidebar 164x314; logo on white.
function wizardImage(size, imgW, imgH) {
  const canvas = Buffer.alloc(imgW * imgH * 4); // transparent; BMP blends over bg
  const off = Math.round((size / GRID) * 1); // 1 cell transparent margin already in art
  const x0 = Math.floor((imgW - size) / 2);
  const y0 = Math.floor((imgH - size) / 2);
  const art = rasterize(MATRIX, size / GRID);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const si = (y * size + x) * 4;
      const di = ((y0 + y) * imgW + (x0 + x)) * 4;
      canvas[di] = art.rgba[si];
      canvas[di + 1] = art.rgba[si + 1];
      canvas[di + 2] = art.rgba[si + 2];
      canvas[di + 3] = art.rgba[si + 3];
    }
  }
  return { width: imgW, height: imgH, rgba: canvas };
}

const WHITE = [255, 255, 255];
const headerBmp = encodeBmp({ ...wizardImage(48, 150, 57), bg: WHITE });
const sidebarBmp = encodeBmp({ ...wizardImage(96, 164, 314), bg: WHITE });
writeFileSync(join(ROOT, 'src-tauri', 'icons', 'header.bmp'), headerBmp);
writeFileSync(join(ROOT, 'src-tauri', 'icons', 'sidebar.bmp'), sidebarBmp);

console.log(
  `icons OK: logo.svg, icon.ico (${ico.length} bytes, 6 sizes), 32x32.png, 128x128.png, 128x128@2x.png, header.bmp, sidebar.bmp`,
);
