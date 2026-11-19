import { NavLink, Outlet } from "react-router";
import { useFleetSummary } from "../api/hooks";
import { useIngestTicks } from "../live/useLive";

function Mark() {
  return (
    <svg width="26" height="26" viewBox="0 0 32 32" aria-hidden>
      <circle cx="16" cy="16" r="15" fill="#172B3A" />
      <path d="M4 16h24M16 4c-5 6-5 18 0 24M16 4c5 6 5 18 0 24" stroke="#C9D3D8" strokeWidth="1.5" fill="none" />
      <circle cx="22" cy="11" r="3" fill="#A3195B" />
    </svg>
  );
}

export function Layout() {
  const summary = useFleetSummary();
  const { state } = useIngestTicks();
  const critical = summary.data?.openAlerts.critical ?? 0;

  return (
    <div className="shell">
      <nav className="sidebar" aria-label="Main">
        <NavLink to="/" className="brand" end>
          <Mark />
          <span>Fleet Console</span>
        </NavLink>

        <div className="nav-group">
          <h2>Operations</h2>
          <ul>
            <li>
              <NavLink to="/" end className="nav-link">
                Overview
              </NavLink>
            </li>
            <li>
              <NavLink to="/terminals" className="nav-link">
                Terminals
              </NavLink>
            </li>
            <li>
              <NavLink to="/alerts" className="nav-link">
                Alerts
                {critical > 0 && (
                  <span className="nav-count" aria-label={`${critical} critical`}>
                    {critical}
                  </span>
                )}
              </NavLink>
            </li>
          </ul>
        </div>

        <div className="nav-group">
          <h2>Sales</h2>
          <ul>
            <li>
              <NavLink to="/sales" className="nav-link">
                Revenue
              </NavLink>
            </li>
            <li>
              <NavLink to="/customers" className="nav-link">
                Customers
              </NavLink>
            </li>
          </ul>
        </div>

        <div className="sidebar-foot">
          <span className={`live-dot ${state}`} aria-hidden />
          {state === "open" ? "Live feed connected" : state === "connecting" ? "Connecting…" : "Live feed offline"}
        </div>
      </nav>

      <main className="main">
        <Outlet />
      </main>
    </div>
  );
}
