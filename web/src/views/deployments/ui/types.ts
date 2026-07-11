import type { AppDetail, AppHistoryItem, AppSummary, DeploymentRequest } from "@/entities/app";
import type { GitSource } from "@/entities/git-source";

export type Notice = {
  tone: "info" | "ok" | "error";
  text: string;
};

export type ConsoleSection = "deployments" | "sources" | "releases" | "audit" | "settings";

export type DetailTab = "contract" | "history" | "source";

export type DetailPage =
  | { kind: "fcode"; sourceID: number }
  | { kind: "request"; requestID: string };

export type DeploymentSelection = {
  source: GitSource | null;
  app: AppSummary | null;
  detail: AppDetail | null;
  history: AppHistoryItem[];
  sourceFiles: Record<string, string>;
};

export type DeploymentRequestAction = {
  request: DeploymentRequest;
  source: GitSource | null;
};
