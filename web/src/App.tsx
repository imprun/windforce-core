import { matchRoute, useRouter } from "./lib/router";
import { AppDetailPage } from "./pages/AppDetailPage";
import { AppsPage } from "./pages/AppsPage";
import { JobDetailPage } from "./pages/JobDetailPage";
import { JobsPage } from "./pages/JobsPage";
import { SettingsPage } from "./pages/SettingsPage";

export function App() {
  const { path } = useRouter();

  const appDetail = matchRoute("/apps/:id/:tab?", path);
  if (appDetail) {
    const sourceID = Number(appDetail.id);
    if (Number.isFinite(sourceID) && sourceID > 0) {
      return <AppDetailPage sourceID={sourceID} tab={appDetail.tab || "overview"} />;
    }
  }

  const jobDetail = matchRoute("/jobs/:id", path);
  if (jobDetail) return <JobDetailPage jobID={jobDetail.id} />;

  if (matchRoute("/jobs", path)) return <JobsPage />;
  if (matchRoute("/settings", path)) return <SettingsPage />;
  return <AppsPage />;
}
