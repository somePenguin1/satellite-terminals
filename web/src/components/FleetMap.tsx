import { useEffect, useMemo, useRef, useState } from "react";
import { feature } from "topojson-client";
import type { Topology, GeometryCollection } from "topojson-specification";
import type { Feature, FeatureCollection, MultiPolygon, Polygon } from "geojson";
import type { FleetMap as FleetMapData, Health } from "../api/types";
import { MAP_BOUNDS, project } from "../lib/chart";
import { HEALTH_COLOR, HEALTH_LABEL } from "../lib/health";
import { useElementWidth } from "../lib/useElementWidth";
import "./FleetMap.css";

type Ring = [number, number][];

/** Land outlines load lazily (≈60 KB) so the first paint isn't blocked on geography. */
function useLandRings(): Ring[] {
  const [rings, setRings] = useState<Ring[]>([]);
  useEffect(() => {
    let alive = true;
    import("world-atlas/land-110m.json").then((mod) => {
      const topo = (mod.default ?? mod) as unknown as Topology<{ land: GeometryCollection }>;
      const geo = feature(topo, topo.objects.land) as FeatureCollection<Polygon | MultiPolygon> | Feature<MultiPolygon>;
      const features = "features" in geo ? geo.features : [geo];
      const out: Ring[] = [];
      for (const f of features) {
        const g = f.geometry;
        const polys = g.type === "Polygon" ? [g.coordinates] : g.coordinates;
        for (const poly of polys) for (const ring of poly) out.push(ring as Ring);
      }
      if (alive) setRings(out);
    });
    return () => {
      alive = false;
    };
  }, []);
  return rings;
}

// Problems are drawn last and larger so they stand out against thousands of healthy dots.
const DRAW_ORDER: Health[] = ["inactive", "healthy", "offline", "degraded", "down"];
const RADIUS: Record<Health, number> = { inactive: 1.6, healthy: 1.9, offline: 2.4, degraded: 2.8, down: 3.2 };

interface Props {
  data: FleetMapData | undefined;
  onSelect: (terminalId: string) => void;
  highlight?: Health | null;
}

export function FleetMap({ data, onSelect, highlight = null }: Props) {
  const [wrapRef, width] = useElementWidth<HTMLDivElement>(900);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rings = useLandRings();
  const [hover, setHover] = useState<{ i: number; x: number; y: number } | null>(null);
  const height = Math.round(Math.max(220, Math.min(520, width * 0.42)));

  // Screen positions, recomputed only when data or size change; reused for hit-testing.
  const points = useMemo(() => {
    if (!data) return [];
    return data.ids.map((_, i) => project(data.lat[i] ?? 0, data.lon[i] ?? 0, width, height));
  }, [data, width, height]);

  useEffect(() => {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);

    // Graticule every 30°, equator slightly heavier.
    ctx.lineWidth = 1;
    for (let lon = -150; lon <= 150; lon += 30) {
      const [x] = project(0, lon, width, height);
      ctx.strokeStyle = "#e3e9eb";
      ctx.beginPath();
      ctx.moveTo(Math.round(x) + 0.5, 0);
      ctx.lineTo(Math.round(x) + 0.5, height);
      ctx.stroke();
    }
    for (let lat = -30; lat <= 60; lat += 30) {
      const [, y] = project(lat, 0, width, height);
      ctx.strokeStyle = lat === 0 ? "#c9d3d8" : "#e3e9eb";
      ctx.beginPath();
      ctx.moveTo(0, Math.round(y) + 0.5);
      ctx.lineTo(width, Math.round(y) + 0.5);
      ctx.stroke();
    }

    ctx.fillStyle = "#dde4e7";
    for (const ring of rings) {
      ctx.beginPath();
      ring.forEach(([lon, lat], i) => {
        const [x, y] = project(lat, lon, width, height);
        if (i === 0) ctx.moveTo(x, y);
        else ctx.lineTo(x, y);
      });
      ctx.closePath();
      ctx.fill();
    }

    if (!data) return;
    for (const h of DRAW_ORDER) {
      const dim = highlight !== null && highlight !== h;
      ctx.fillStyle = HEALTH_COLOR[h];
      ctx.globalAlpha = dim ? 0.12 : h === "healthy" ? 0.7 : 0.95;
      const r = RADIUS[h];
      ctx.beginPath();
      for (let i = 0; i < data.ids.length; i++) {
        if (data.health[i] !== h) continue;
        const p = points[i];
        if (!p) continue;
        ctx.moveTo(p[0] + r, p[1]);
        ctx.arc(p[0], p[1], r, 0, Math.PI * 2);
      }
      ctx.fill();
    }
    ctx.globalAlpha = 1;
  }, [data, points, rings, width, height, highlight]);

  function nearest(e: React.MouseEvent<HTMLCanvasElement>): number | null {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    let best = -1;
    let bestD = 64; // within 8 px
    for (let i = 0; i < points.length; i++) {
      const p = points[i];
      if (!p) continue;
      const d = (p[0] - x) ** 2 + (p[1] - y) ** 2;
      // Prefer unhealthy terminals when dots overlap.
      const bias = data?.health[i] === "healthy" || data?.health[i] === "inactive" ? 4 : 0;
      if (d + bias < bestD) {
        bestD = d + bias;
        best = i;
      }
    }
    return best >= 0 ? best : null;
  }

  const hovered = hover && data ? { id: data.ids[hover.i], health: data.health[hover.i] } : null;

  return (
    <div className="fleet-map" ref={wrapRef}>
      <canvas
        ref={canvasRef}
        style={{ width, height }}
        role="img"
        aria-label={`Map of ${data?.ids.length ?? 0} terminals coloured by health. Select a dot to open the terminal.`}
        onMouseMove={(e) => {
          const i = nearest(e);
          const p = i === null ? null : points[i];
          setHover(i === null || !p ? null : { i, x: p[0], y: p[1] });
        }}
        onMouseLeave={() => setHover(null)}
        onClick={(e) => {
          const i = nearest(e);
          const id = i === null ? undefined : data?.ids[i];
          if (id) onSelect(id);
        }}
        className={hover ? "pointing" : undefined}
      />
      {hover && hovered?.id && hovered.health && (
        <div
          className="map-tip"
          style={{ left: Math.min(hover.x + 12, width - 170), top: Math.max(hover.y - 40, 4) }}
          aria-hidden
        >
          <strong>{hovered.id}</strong>
          <span className={`health ${hovered.health}`}>{HEALTH_LABEL[hovered.health]}</span>
        </div>
      )}
      <span className="map-axis map-axis-n">{MAP_BOUNDS.latMax}°N</span>
      <span className="map-axis map-axis-s">{-MAP_BOUNDS.latMin}°S</span>
    </div>
  );
}
