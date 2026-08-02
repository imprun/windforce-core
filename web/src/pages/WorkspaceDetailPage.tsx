import { useEffect } from "react";
import { Layout } from "../components/Layout";
import { Loading } from "../components/ui";
import { useApp } from "../lib/app-context";
import { useRouter } from "../lib/router";
import { translate } from "../shared/i18n";

export function workspaceDetailTarget(tab: string): string {
  if (tab === "audit") return "/audit";
  return "/settings/workspace";
}

export function WorkspaceDetailPage({ workspaceID, tab }: { workspaceID: string; tab: string }) {
  const { settings, updateSettings } = useApp();
  const { navigate } = useRouter();

  useEffect(() => {
    if (settings.workspace !== workspaceID) {
      updateSettings({ ...settings, workspace: workspaceID });
    }
    navigate(workspaceDetailTarget(tab), { replace: true });
  }, [navigate, settings, tab, updateSettings, workspaceID]);

  return (
    <Layout
      title={translate("workspaces.settings")}
      subtitle={translate("workspaces.settingsRedirect")}
    >
      <Loading label={translate("workspaces.openingSettings")} />
    </Layout>
  );
}
