export interface TimeRangeValue {
    start: string;
    end: string;
    timezone: string;
    outside: boolean;
}

export const DEFAULT_TIME_RANGE: TimeRangeValue = {
    start: '09:00',
    end: '17:00',
    timezone: 'UTC',
    outside: false,
};

const isTimeOfDay = (value: string): boolean => /^([01]\d|2[0-3]):[0-5]\d$/.test(value);

export const parseTimeRange = (value: string): TimeRangeValue | null => {
    try {
        const parsed: unknown = JSON.parse(value);
        if (!parsed || typeof parsed !== 'object') return null;
        const candidate = parsed as Partial<TimeRangeValue>;
        if (
            !isTimeOfDay(candidate.start ?? '') ||
            !isTimeOfDay(candidate.end ?? '') ||
            !candidate.timezone ||
            typeof candidate.outside !== 'boolean'
        ) {
            return null;
        }
        return {
            start: candidate.start!,
            end: candidate.end!,
            timezone: candidate.timezone!,
            outside: candidate.outside,
        };
    } catch {
        return null;
    }
};

export const serializeTimeRange = (value: TimeRangeValue): string => JSON.stringify(value);

export const isValidTimeRange = (value: TimeRangeValue | null): value is TimeRangeValue =>
    value !== null && isTimeOfDay(value.start) && isTimeOfDay(value.end) && value.start !== value.end && !!value.timezone;

export const formatTimeRange = (value: string): string | null => {
    const range = parseTimeRange(value);
    if (!range) return null;
    return `${range.outside ? 'Outside' : 'During'} ${range.start}–${range.end} · ${timezoneLabel(range.timezone)}`;
};

// A curated set of common IANA zones — one representative city per UTC offset,
// matching the convention used by most scheduling UIs (Google Calendar, GitHub
// Actions cron, etc.) instead of the full ~400-entry IANA database.
const COMMON_TIMEZONES: string[] = [
    'UTC',
    'Pacific/Midway',
    'Pacific/Honolulu',
    'America/Anchorage',
    'America/Los_Angeles',
    'America/Denver',
    'America/Chicago',
    'America/New_York',
    'America/Sao_Paulo',
    'Atlantic/Azores',
    'Europe/London',
    'Europe/Paris',
    'Europe/Berlin',
    'Europe/Athens',
    'Europe/Moscow',
    'Asia/Dubai',
    'Asia/Karachi',
    'Asia/Kolkata',
    'Asia/Dhaka',
    'Asia/Bangkok',
    'Asia/Shanghai',
    'Asia/Tokyo',
    'Australia/Sydney',
    'Pacific/Auckland',
];

const zoneCity = (timezone: string): string => timezone.split('/').pop()?.replace(/_/g, ' ') ?? timezone;

const zoneOffsetMinutes = (timezone: string): number => {
    try {
        const parts = new Intl.DateTimeFormat('en-US', { timeZone: timezone, timeZoneName: 'longOffset' }).formatToParts(new Date());
        const offset = parts.find((part) => part.type === 'timeZoneName')?.value ?? 'GMT+0';
        const match = /GMT([+-])(\d{1,2})(?::?(\d{2}))?/.exec(offset);
        if (!match) return 0;
        const sign = match[1] === '-' ? -1 : 1;
        return sign * (Number(match[2]) * 60 + Number(match[3] ?? 0));
    } catch {
        return 0;
    }
};

export const timezoneLabel = (timezone: string): string => {
    const minutes = zoneOffsetMinutes(timezone);
    const sign = minutes < 0 ? '-' : '+';
    const abs = Math.abs(minutes);
    const offset = `UTC${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`;
    return timezone === 'UTC' ? offset : `${offset} ${zoneCity(timezone)}`;
};

export const timezoneOptions = (): string[] => COMMON_TIMEZONES.slice().sort((a, b) => zoneOffsetMinutes(a) - zoneOffsetMinutes(b));
