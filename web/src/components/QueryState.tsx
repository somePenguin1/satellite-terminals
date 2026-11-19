import { ApiError } from "../api/client";

export function LoadingRows({ rows = 5 }: { rows?: number }) {
  return (
    <div aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="skeleton" style={{ margin: "10px 0", width: `${90 - (i % 3) * 12}%` }} />
      ))}
    </div>
  );
}

export function ErrorNotice({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message =
    error instanceof ApiError
      ? error.status >= 500 || error.status === 0
        ? `The fleet API returned an error (${error.status}). ${error.message}`
        : error.message
      : "The fleet API could not be reached. Check that the fleet-api service is running.";
  return (
    <div className="notice error" role="alert">
      <p>{message}</p>
      {onRetry && (
        <button type="button" className="btn secondary small" style={{ marginTop: 8 }} onClick={onRetry}>
          Try again
        </button>
      )}
    </div>
  );
}
