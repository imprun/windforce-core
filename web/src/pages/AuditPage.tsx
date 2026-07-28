import { type FormEvent, useState } from "react";
import { Layout } from "../components/Layout";
import { ErrorNotice, Loading, Panel, SelectControl } from "../components/ui";
import { AuditEventTable } from "../features/AuditEventTable";
import { useApp, useAsync } from "../lib/app-context";

export function AuditPage() {
  const { api } = useApp();
  const [category, setCategory] = useState("");
  const [appKey, setAppKey] = useState("");
  const [clientID, setClientID] = useState("");
  const [actorDraft, setActorDraft] = useState("");
  const [actor, setActor] = useState("");

  const options = useAsync(async () => {
    const [apps, clients] = await Promise.all([api.apps(), api.clients()]);
    return { apps: apps.apps || [], clients };
  }, [api]);
  const selectedApp = options.data?.apps.find((app) => app.app_key === appKey);
  const events = useAsync(
    () =>
      api.auditEvents({
        category,
        appKey,
        clientID,
        actor,
        gitSourceID: selectedApp?.git_source_id,
        limit: 250,
      }),
    [api, category, appKey, clientID, actor, selectedApp?.git_source_id],
  );

  function applyActor(event: FormEvent) {
    event.preventDefault();
    setActor(actorDraft.trim());
  }

  function resetFilters() {
    setCategory("");
    setAppKey("");
    setClientID("");
    setActorDraft("");
    setActor("");
  }

  const filtered = Boolean(category || appKey || clientID || actor);

  return (
    <Layout
      title="Audit"
      subtitle="Workspace change history across identity, access, repositories, releases, clients, input settings, and webhooks."
      actions={
        <button className="button" type="button" onClick={() => events.reload()}>
          Refresh
        </button>
      }
    >
      <Panel
        title="Workspace activity"
        subtitle={
          events.data
            ? `${events.data.length} most recent event${events.data.length === 1 ? "" : "s"}`
            : "Loading events…"
        }
        actions={
          filtered ? (
            <button className="button small" type="button" onClick={resetFilters}>
              Reset filters
            </button>
          ) : null
        }
      >
        <form className="auditFilters" onSubmit={applyActor}>
          <label className="filterField">
            <span>Category</span>
            <SelectControl
              value={category}
              onChange={setCategory}
              ariaLabel="Audit category"
              options={[
                { value: "", label: "All categories" },
                { value: "workspace", label: "Workspace" },
                { value: "repository", label: "Repository" },
                { value: "release", label: "Release" },
                { value: "client", label: "Client Registry" },
                { value: "input_settings", label: "Input Settings" },
                { value: "webhook", label: "Webhooks" },
              ]}
            />
          </label>
          <label className="filterField">
            <span>App</span>
            <SelectControl
              value={appKey}
              onChange={setAppKey}
              ariaLabel="Audit app"
              options={[
                { value: "", label: "All apps" },
                ...(options.data?.apps || []).map((app) => ({
                  value: app.app_key,
                  label: app.app_key,
                })),
              ]}
            />
          </label>
          <label className="filterField">
            <span>Client</span>
            <SelectControl
              value={clientID}
              onChange={setClientID}
              ariaLabel="Audit client"
              options={[
                { value: "", label: "All clients" },
                ...(options.data?.clients || []).map((client) => ({
                  value: client.id,
                  label: client.name,
                })),
              ]}
            />
          </label>
          <label className="filterField auditActorFilter">
            <span>Actor</span>
            <span className="filterInputAction">
              <input
                value={actorDraft}
                placeholder="Name or account"
                onChange={(event) => setActorDraft(event.target.value)}
              />
              <button className="button" type="submit">
                Apply
              </button>
            </span>
          </label>
        </form>

        {options.error ? <ErrorNotice message={options.error} onRetry={options.reload} /> : null}
        {events.error ? <ErrorNotice message={events.error} onRetry={events.reload} /> : null}
        {events.loading && !events.data ? <Loading /> : null}
        {events.data ? <AuditEventTable events={events.data} /> : null}
      </Panel>
    </Layout>
  );
}
