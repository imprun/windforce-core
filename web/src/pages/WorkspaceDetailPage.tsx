import { useEffect } from "react";
import { Layout } from "../components/Layout";
import { Loading } from "../components/ui";
import { useApp } from "../lib/app-context";
import { useRouter } from "../lib/router";

export function workspaceDetailTarget(tab: string): string {
  if (tab === "access") return "/settings/access";
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
    <Layout title="Workspace settings" subtitle="Moving workspace administration to Settings.">
      <Loading label="Opening workspace settings…" />
    </Layout>
  );
}
