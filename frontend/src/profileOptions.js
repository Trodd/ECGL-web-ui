// Shared option lists for the profile fields (role / device / timezone).
// Used by the Register page and by Player Settings so they can never drift.

export const ROLE_OPTIONS = [
  { value: "Player", label: "Player" },
  { value: "League Sub", label: "League Sub" },
];

export const DEVICE_OPTIONS = [
  { value: "rift", label: "PCVR (SteamVR / Oculus PC)" },
  { value: "quest_link", label: "Quest + Link/AirLink" },
  { value: "quest_native", label: "Quest Native (standalone)" },
];

export const TIMEZONE_GROUPS = [
  {
    label: "North America",
    options: [
      { value: "US/Eastern", label: "US/Eastern (EST/EDT)" },
      { value: "US/Central", label: "US/Central (CST/CDT)" },
      { value: "US/Mountain", label: "US/Mountain (MST/MDT)" },
      { value: "US/Arizona", label: "US/Arizona (no DST)" },
      { value: "US/Pacific", label: "US/Pacific (PST/PDT)" },
      { value: "US/Alaska", label: "US/Alaska" },
      { value: "US/Hawaii", label: "US/Hawaii" },
      { value: "Canada/Atlantic", label: "Canada/Atlantic" },
      { value: "Canada/Newfoundland", label: "Canada/Newfoundland" },
    ],
  },
  {
    label: "Europe",
    options: [
      { value: "Europe/London", label: "Europe/London (UK)" },
      { value: "Europe/Dublin", label: "Europe/Dublin (Ireland)" },
      { value: "Europe/Lisbon", label: "Europe/Lisbon (Portugal)" },
      { value: "Europe/Paris", label: "Europe/Paris (France)" },
      { value: "Europe/Berlin", label: "Europe/Berlin (Germany)" },
      { value: "Europe/Rome", label: "Europe/Rome (Italy)" },
      { value: "Europe/Madrid", label: "Europe/Madrid (Spain)" },
      { value: "Europe/Amsterdam", label: "Europe/Amsterdam (Netherlands)" },
      { value: "Europe/Brussels", label: "Europe/Brussels (Belgium)" },
      { value: "Europe/Vienna", label: "Europe/Vienna (Austria)" },
      { value: "Europe/Oslo", label: "Europe/Oslo (Norway)" },
      { value: "Europe/Stockholm", label: "Europe/Stockholm (Sweden)" },
      { value: "Europe/Copenhagen", label: "Europe/Copenhagen (Denmark)" },
      { value: "Europe/Helsinki", label: "Europe/Helsinki (Finland)" },
      { value: "Europe/Athens", label: "Europe/Athens (Greece)" },
      { value: "Europe/Sofia", label: "Europe/Sofia (Bulgaria)" },
      { value: "Europe/Warsaw", label: "Europe/Warsaw (Poland)" },
      { value: "Europe/Budapest", label: "Europe/Budapest (Hungary)" },
      { value: "Europe/Prague", label: "Europe/Prague (Czechia)" },
      { value: "Europe/Bucharest", label: "Europe/Bucharest (Romania)" },
      { value: "Europe/Moscow", label: "Europe/Moscow (Russia)" },
    ],
  },
];

// Human-readable label for a stored value (falls back to the raw value).
export function optionLabel(options, value) {
  const found = options.find((o) => o.value === value);
  return found ? found.label : value;
}

export function timezoneLabel(value) {
  for (const g of TIMEZONE_GROUPS) {
    const found = g.options.find((o) => o.value === value);
    if (found) return found.label;
  }
  return value;
}
