"use client";

import type { DeploymentRequest } from "@/entities/app";
import type { DetailPage } from "./types";
import { formatDate, shortID } from "@/shared/lib/format";
import {
  ContractTab,
  EmptyLine,
  Field,
  LatestAudit,
  PanelHeader,
  ReadinessPanel,
  SourceSnapshotPanel,
  type CommonProps,
} from "./DeploymentPanels";

export function FCodeDetailSection(props: CommonProps & { detailPage: Extract<DetailPage, { kind: "fcode" }> }) {
  const source = props.sources.find((item) => item.id === props.detailPage.sourceID) || null;
  const app = source ? props.apps.find((item) => item.git_source_id === source.id) || null : null;
  const requests = source ? sortRequests(props.deploymentRequests.filter((request) => request.git_source_id === source.id)) : [];
  const pendingRequest = requests.find((request) => request.status === "pending") || null;

  if (!source) {
    return <DetailNotFound title="FCode source not found" onBack={props.onBackToList} />;
  }

  return (
    <div id="fcodeDetailPage" className="detailPage">
      <section className="workspacePanel detailHero">
        <div className="detailHeroMain">
          <button className="button compactButton" type="button" onClick={props.onBackToList}>Back to deployments</button>
          <div>
            <span className="eyebrow">FCode detail</span>
            <h2>{source.name}</h2>
            <p>{source.repo_url}</p>
          </div>
        </div>
        <div className="detailHeroActions">
          <span className={source.last_synced_commit ? "badge ok" : "badge warn"}>{source.last_synced_commit ? "deployed" : "registered"}</span>
          <button className="button primary" type="button" onClick={() => props.onRequestDeploy(source)}>Request deployment</button>
          {pendingRequest ? <button className="button" type="button" onClick={() => props.onOpenRequestDetail(pendingRequest.id)}>Open pending request</button> : null}
        </div>
      </section>

      <div className="detailLayout">
        <div className="detailMain">
          <section className="workspacePanel">
            <PanelHeader eyebrow="Worker contract" title={app?.app_key || "No active contract"} description={app?.entrypoint || "No deployed app contract for this source."} />
            <ContractTab app={app} detail={props.detail} />
          </section>

          <section className="workspacePanel">
            <PanelHeader eyebrow="Deployment requests" title="Request history" description="Requests are tied to a pinned commit and reviewed before publication." />
            <RequestList requests={requests} onOpenRequestDetail={props.onOpenRequestDetail} onReviewRequest={props.onReviewRequest} />
          </section>

          <section className="workspacePanel">
            <PanelHeader eyebrow="Source snapshot" title="Materialized files" description="The active contract was generated from this source snapshot." />
            <SourceSnapshotPanel files={props.sourceFiles} compact />
          </section>
        </div>

        <aside className="detailAside">
          <section className="workspacePanel">
            <PanelHeader eyebrow="Source registration" title="Git source" description={source.kind || "git"} />
            <div className="sourceDetailGrid">
              <Field label="Branch" value={source.branch || "main"} />
              <Field label="Subpath" value={source.subpath || "root"} />
              <Field label="Credential" value={source.creds_ref ? "configured" : "public repository"} />
              <Field label="Source ID" value={String(source.id)} />
              <Field label="Last deployed" value={formatDate(source.last_synced_at)} />
              <Field label="Last commit" value={shortID(source.last_synced_commit, 16)} />
            </div>
          </section>

          <section className="workspacePanel">
            <ReadinessPanel source={source} app={app} actor={props.actor} liveWorkers={props.liveWorkers} />
          </section>

          <section className="workspacePanel">
            <LatestAudit history={props.history} />
          </section>

          <section className="workspacePanel dangerZonePanel">
            <div>
              <strong>Source registration</strong>
              <p>Remove only when this FCode should no longer be deployable from the control plane.</p>
            </div>
            <button className="button dangerGhost" type="button" onClick={() => props.onRemove(source)}>Remove Source</button>
          </section>
        </aside>
      </div>
    </div>
  );
}

export function DeploymentRequestDetailSection(props: CommonProps & { detailPage: Extract<DetailPage, { kind: "request" }> }) {
  const request = props.deploymentRequests.find((item) => item.id === props.detailPage.requestID) || null;
  const source = request ? props.sources.find((item) => item.id === request.git_source_id) || null : null;
  const app = source ? props.apps.find((item) => item.git_source_id === source.id) || null : null;

  if (!request) {
    return <DetailNotFound title="Deployment request not found" onBack={props.onBackToList} />;
  }

  return (
    <div id="requestDetailPage" className="detailPage">
      <section className="workspacePanel detailHero">
        <div className="detailHeroMain">
          <button className="button compactButton" type="button" onClick={props.onBackToList}>Back to deployments</button>
          <div>
            <span className="eyebrow">Deployment request</span>
            <h2>{request.source_name}</h2>
            <p>{shortID(request.id, 18)} / {request.requested_by || "-"}</p>
          </div>
        </div>
        <div className="detailHeroActions">
          <span className={`badge ${statusTone(request.status)}`}>{request.status}</span>
          {source ? <button className="button" type="button" onClick={() => props.onOpenFCodeDetail(source.id)}>Open FCode detail</button> : null}
          {request.status === "pending" ? <button className="button primary" type="button" onClick={() => props.onReviewRequest(request)}>Review request</button> : null}
        </div>
      </section>

      <div className="detailLayout requestDetailLayout">
        <div className="detailMain">
          <section className="workspacePanel">
            <PanelHeader eyebrow="Request timeline" title="Deployment decision" description="The target commit is fixed when the request is created." />
            <RequestTimeline request={request} />
          </section>

          <section className="workspacePanel">
            <PanelHeader eyebrow="Target release" title={request.app_key || "App pending"} description={request.entrypoint || "Entrypoint not set"} />
            <div className="sourceDetailGrid">
              <Field label="Target commit" value={shortID(request.target_commit, 18)} />
              <Field label="Current commit" value={shortID(request.current_commit, 18)} />
              <Field label="Branch" value={request.branch || "main"} />
              <Field label="Subpath" value={request.subpath || "root"} />
              <Field label="Actions" value={String(request.actions_count || 0)} />
              <Field label="Deployment ID" value={shortID(request.deployment_id, 16)} />
            </div>
          </section>

          <section className="workspacePanel">
            <PanelHeader eyebrow="Review notes" title="Requester and operator messages" />
            <div className="messageGrid">
              <MessageBlock label="Requester" actor={request.requested_by || "-"} message={request.request_message || "-"} />
              <MessageBlock label="Operator" actor={request.reviewed_by || request.deployed_by || "-"} message={request.operator_message || "-"} />
            </div>
          </section>
        </div>

        <aside className="detailAside">
          <section className="workspacePanel">
            <PanelHeader eyebrow="Source" title={source?.name || request.source_name} description={request.repo_url} />
            <div className="sourceDetailGrid">
              <Field label="Source ID" value={String(request.git_source_id)} />
              <Field label="Credential" value={source?.creds_ref ? "configured" : "public repository"} />
              <Field label="Source state" value={source?.last_synced_commit ? "deployed" : "registered"} />
              <Field label="Last deployed" value={formatDate(source?.last_synced_at)} />
            </div>
          </section>

          <section className="workspacePanel">
            <ReadinessPanel source={source} app={app} actor={props.actor} liveWorkers={props.liveWorkers} />
          </section>

          <section className="workspacePanel">
            <LatestAudit history={props.history} />
          </section>
        </aside>
      </div>
    </div>
  );
}

function RequestList({ requests, onOpenRequestDetail, onReviewRequest }: { requests: DeploymentRequest[]; onOpenRequestDetail: (requestID: string) => void; onReviewRequest: (request: DeploymentRequest) => void }) {
  return (
    <div className="requestDetailList">
      {requests.length === 0 ? <EmptyLine>No deployment requests for this FCode.</EmptyLine> : null}
      {requests.map((request) => (
        <article className="requestDetailItem" key={request.id}>
          <div>
            <span className={`badge ${statusTone(request.status)}`}>{request.status}</span>
            <strong>{shortID(request.target_commit, 16)}</strong>
            <p>{request.requested_by || "-"} / {formatDate(request.created_at)}</p>
          </div>
          <div className="rowButtons">
            <button className="button compactButton" type="button" onClick={() => onOpenRequestDetail(request.id)}>Details</button>
            {request.status === "pending" ? <button className="button primary compactButton" type="button" onClick={() => onReviewRequest(request)}>Review</button> : null}
          </div>
        </article>
      ))}
    </div>
  );
}

function RequestTimeline({ request }: { request: DeploymentRequest }) {
  const reviewedAt = request.status === "pending" ? "" : request.updated_at;
  const finalLabel = request.status === "rejected" ? "Rejected" : request.status === "deployed" ? "Deployed" : "Not published";
  const finalDetail = request.status === "pending"
    ? "Review required before deployment."
    : request.status === "deployed"
      ? `${shortID(request.deployed_commit || request.target_commit, 18)} / ${formatDate(request.deployed_at || request.updated_at)}`
      : request.operator_message || "-";
  const finalActor = request.reviewed_by || request.deployed_by || "-";
  return (
    <ol className="timelineList">
      <TimelineItem tone="ok" title="Requested" detail={`${request.requested_by || "-"} / ${formatDate(request.created_at)}`} />
      <TimelineItem tone={request.status === "pending" ? "warn" : "ok"} title="Operator review" detail={request.status === "pending" ? "Pending" : `${finalActor} / ${formatDate(reviewedAt)}`} />
      <TimelineItem tone={request.status === "rejected" ? "error" : request.status === "deployed" ? "ok" : "neutral"} title={finalLabel} detail={finalDetail} />
    </ol>
  );
}

function TimelineItem({ tone, title, detail }: { tone: "ok" | "warn" | "error" | "neutral"; title: string; detail: string }) {
  return (
    <li className="timelineItem">
      <span className={`statusDot ${tone}`} aria-hidden="true" />
      <div>
        <strong>{title}</strong>
        <p>{detail}</p>
      </div>
    </li>
  );
}

function MessageBlock({ label, actor, message }: { label: string; actor: string; message: string }) {
  return (
    <article className="messageBlock">
      <span className="eyebrow">{label}</span>
      <strong>{actor}</strong>
      <p>{message}</p>
    </article>
  );
}

function DetailNotFound({ title, onBack }: { title: string; onBack: () => void }) {
  return (
    <section className="workspacePanel emptyState">
      <span className="eyebrow">Detail</span>
      <h2>{title}</h2>
      <button className="button primary" type="button" onClick={onBack}>Back to deployments</button>
    </section>
  );
}

function sortRequests(requests: DeploymentRequest[]) {
  return [...requests].sort((a, b) => {
    if (a.status === "pending" && b.status !== "pending") return -1;
    if (a.status !== "pending" && b.status === "pending") return 1;
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });
}

function statusTone(status: DeploymentRequest["status"]) {
  if (status === "pending") return "warn";
  if (status === "deployed") return "ok";
  return "neutral";
}
