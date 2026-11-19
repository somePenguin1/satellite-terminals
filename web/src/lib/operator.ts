import { useState } from "react";

const KEY = "satfleet.operator";
const DEFAULT = "ops-oncall";

/** The operator name for audit headers, readable outside React. */
export function currentOperator(): string {
  return read();
}

function read(): string {
  try {
    return window.localStorage.getItem(KEY) || DEFAULT;
  } catch {
    return DEFAULT;
  }
}

/** Name recorded on acknowledgements and config changes. Real deployments take it from SSO. */
export function useOperator(): [string, (name: string) => void] {
  const [name, setName] = useState(read);
  return [
    name,
    (next: string) => {
      const clean = next.trim() || DEFAULT;
      setName(clean);
      try {
        window.localStorage.setItem(KEY, clean);
      } catch {
        // Storage unavailable (private mode); keep the name for this session only.
      }
    },
  ];
}
