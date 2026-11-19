import type { RouteObject } from "react-router";
import { Link } from "react-router";
import { Layout } from "./components/Layout";
import { AlertsPage } from "./pages/AlertsPage";
import { CustomerDetailPage } from "./pages/CustomerDetailPage";
import { CustomersPage } from "./pages/CustomersPage";
import { OverviewPage } from "./pages/OverviewPage";
import { SalesPage } from "./pages/SalesPage";
import { TerminalDetailPage } from "./pages/TerminalDetailPage";
import { TerminalsPage } from "./pages/TerminalsPage";

function NotFound() {
  return (
    <div className="empty">
      <h1>Page not found</h1>
      <p style={{ marginTop: 8 }}>
        This address doesn't match any page. <Link to="/">Go to the fleet overview</Link>.
      </p>
    </div>
  );
}

export const routes: RouteObject[] = [
  {
    path: "/",
    element: <Layout />,
    children: [
      { index: true, element: <OverviewPage /> },
      { path: "terminals", element: <TerminalsPage /> },
      { path: "terminals/:id", element: <TerminalDetailPage /> },
      { path: "alerts", element: <AlertsPage /> },
      { path: "sales", element: <SalesPage /> },
      { path: "customers", element: <CustomersPage /> },
      { path: "customers/:id", element: <CustomerDetailPage /> },
      { path: "*", element: <NotFound /> },
    ],
  },
];
