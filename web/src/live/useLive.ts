import { useEffect, useSyncExternalStore } from "react";
import type { IngestTick, LiveReport } from "../api/types";
import { liveFeed, type ConnectionState } from "./liveFeed";

const EMPTY: LiveReport[] = [];

export function useIngestTicks(): { ticks: IngestTick[]; state: ConnectionState } {
  const feed = liveFeed();
  const ticks = useSyncExternalStore(
    (cb) => feed.subscribe(cb),
    () => feed.ticks,
  );
  const state = useSyncExternalStore(
    (cb) => feed.subscribe(cb),
    () => feed.state,
  );
  return { ticks, state };
}

/** Streams every report for one terminal while the component is mounted. */
export function useLiveTerminal(id: string): LiveReport[] {
  const feed = liveFeed();
  useEffect(() => feed.watchTerminal(id), [feed, id]);
  return useSyncExternalStore(
    (cb) => feed.subscribe(cb),
    () => feed.reports.get(id) ?? EMPTY,
  );
}
