/** Rounds a raw step up to 1, 2, 2.5 or 5 times a power of ten. */
export function niceStep(raw: number): number {
  if (!(raw > 0) || !Number.isFinite(raw)) return 1;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const f = raw / pow;
  const nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10;
  return nice * pow;
}

/** Y-axis domain and ticks that start at zero (all our metrics are non-negative). */
export function niceScale(maxValue: number, tickCount = 4): { max: number; ticks: number[] } {
  const max = maxValue > 0 ? maxValue : 1;
  const step = niceStep(max / tickCount);
  const top = Math.ceil(max / step) * step;
  const ticks: number[] = [];
  for (let v = 0; v <= top + step / 2; v += step) ticks.push(Number(v.toFixed(6)));
  return { max: top, ticks };
}

/** SVG path for a line with gaps where values are null. */
export function linePath(xs: number[], ys: (number | null)[]): string {
  let d = "";
  let pen = false;
  for (let i = 0; i < xs.length; i++) {
    const y = ys[i];
    const x = xs[i];
    if (y === null || y === undefined || x === undefined || !Number.isFinite(y)) {
      pen = false;
      continue;
    }
    d += `${pen ? "L" : "M"}${x.toFixed(1)},${y.toFixed(1)}`;
    pen = true;
  }
  return d;
}

/** Equirectangular projection cropped to inhabited latitudes. */
export const MAP_BOUNDS = { latMin: -58, latMax: 78, lonMin: -180, lonMax: 180 };

export function project(lat: number, lon: number, width: number, height: number): [number, number] {
  const x = ((lon - MAP_BOUNDS.lonMin) / (MAP_BOUNDS.lonMax - MAP_BOUNDS.lonMin)) * width;
  const y = ((MAP_BOUNDS.latMax - lat) / (MAP_BOUNDS.latMax - MAP_BOUNDS.latMin)) * height;
  return [x, y];
}
