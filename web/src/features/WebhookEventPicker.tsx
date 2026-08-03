import { translate } from "../shared/i18n";
import { webhookEventLabel } from "./WebhookStatus";

export const DEFAULT_WEBHOOK_EVENT_TYPES = [
  "windforce.release.published",
  "windforce.release.rolled_back",
];

const EVENT_OPTIONS = [
  {
    group: "release",
    types: ["windforce.release.published", "windforce.release.rolled_back"],
  },
  {
    group: "humanTask",
    types: [
      "windforce.human_task.created",
      "windforce.human_task.decided",
      "windforce.human_task.expired",
      "windforce.human_task.canceled",
    ],
  },
] as const;

export function WebhookEventPicker({
  selected,
  onChange,
  disabled = false,
}: {
  selected: string[];
  onChange: (eventTypes: string[]) => void;
  disabled?: boolean;
}) {
  function toggle(eventType: string) {
    onChange(
      selected.includes(eventType)
        ? selected.filter((candidate) => candidate !== eventType)
        : [...selected, eventType].sort(),
    );
  }

  return (
    <fieldset className="webhookEventPicker" disabled={disabled}>
      <legend>{translate("webhook.events")}</legend>
      <p className="fieldHint">{translate("webhook.eventSelectionHint")}</p>
      <div className="webhookEventList">
        {EVENT_OPTIONS.map((group) => (
          <section className="webhookEventGroup" key={group.group}>
            <h3>
              {translate(`webhook.eventGroup.${group.group}` as Parameters<typeof translate>[0])}
            </h3>
            {group.types.map((eventType) => (
              <label className="webhookEventOption" key={eventType}>
                <input
                  type="checkbox"
                  checked={selected.includes(eventType)}
                  onChange={() => toggle(eventType)}
                />
                <span>
                  <strong>{webhookEventLabel(eventType)}</strong>
                  <small className="mono">{eventType}</small>
                </span>
              </label>
            ))}
          </section>
        ))}
      </div>
    </fieldset>
  );
}
