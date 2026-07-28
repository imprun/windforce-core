import { ChevronLeft, ChevronRight, Lock } from "lucide-react";
import { formatRelative, formatTime } from "../lib/format";
import type { InputSettingGroup } from "../lib/input-setting-groups";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";

export type InputSettingSummaryRow = {
  group: InputSettingGroup;
  label: string;
  subtitle: string;
  href: string;
  coverage: string;
  coverageDetail: string;
};

export function countLabel(count: number, singular: string, plural = `${singular}s`) {
  return `${count} ${count === 1 ? singular : plural}`;
}

export function InputSettingSummaryTable({
  id,
  scopeHeading,
  rows,
}: {
  id: string;
  scopeHeading: string;
  rows: InputSettingSummaryRow[];
}) {
  return (
    <div className="tableWrap">
      <table className="table inputSettingSummaryTable" id={id}>
        <thead>
          <tr>
            <th>{scopeHeading}</th>
            <th>{translate("inputSettings.coverage")}</th>
            <th>{translate("inputSettings.configuredValues")}</th>
            <th>{translate("inputSettings.locked")}</th>
            <th>{translate("inputSettings.lastChange")}</th>
            <th aria-label={translate("inputSettings.open")} />
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.group.key}>
              <td className="inputSettingSummaryScope">
                <Link className="cellTitle" to={row.href}>
                  {row.label}
                </Link>
                <span className="cellSub">{row.subtitle}</span>
              </td>
              <td className="inputSettingSummaryCoverage">
                <span className="cellTitle">{row.coverage}</span>
                <span className="cellSub">{row.coverageDetail}</span>
              </td>
              <td className="inputSettingSummaryValues">
                <span className="cellTitle">
                  {countLabel(
                    row.group.valueCount,
                    translate("inputSettings.value"),
                    translate("inputSettings.values"),
                  )}
                </span>
                <span className="cellSub mono">
                  {row.group.keyNames.join(", ") || translate("inputSettings.noKeys")}
                </span>
              </td>
              <td className="inputSettingSummaryLocked">
                {row.group.lockedCount ? (
                  <span className="lockedCount">
                    <Lock size={14} aria-hidden="true" /> {row.group.lockedCount}
                  </span>
                ) : (
                  "0"
                )}
              </td>
              <td className="inputSettingSummaryUpdated" title={formatTime(row.group.updatedAt)}>
                <span className="cellTitle">{formatRelative(row.group.updatedAt)}</span>
                <span className="cellSub">{row.group.updatedBy}</span>
              </td>
              <td className="rowActions inputSettingSummaryOpen">
                <Link
                  className="button small iconButton"
                  to={row.href}
                  title={translate("inputSettings.view")}
                  aria-label={translate("inputSettings.viewNamed", { name: row.label })}
                >
                  <ChevronRight size={16} aria-hidden="true" />
                </Link>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function SummaryPagination({
  page,
  totalPages,
  totalItems,
  pageSize,
  onChange,
}: {
  page: number;
  totalPages: number;
  totalItems: number;
  pageSize: number;
  onChange: (page: number) => void;
}) {
  if (totalPages <= 1) return null;
  const start = (page - 1) * pageSize + 1;
  const end = Math.min(page * pageSize, totalItems);
  return (
    <nav className="summaryPagination" aria-label={translate("inputSettings.pages")}>
      <span>{translate("inputSettings.range", { start, end, total: totalItems })}</span>
      <div className="summaryPaginationControls">
        <button
          className="button small iconButton"
          type="button"
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
          title={translate("inputSettings.previousPage")}
          aria-label={translate("inputSettings.previousPage")}
        >
          <ChevronLeft size={16} aria-hidden="true" />
        </button>
        <span>{translate("inputSettings.page", { page, total: totalPages })}</span>
        <button
          className="button small iconButton"
          type="button"
          disabled={page >= totalPages}
          onClick={() => onChange(page + 1)}
          title={translate("inputSettings.nextPage")}
          aria-label={translate("inputSettings.nextPage")}
        >
          <ChevronRight size={16} aria-hidden="true" />
        </button>
      </div>
    </nav>
  );
}
