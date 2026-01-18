/* Calendar v3 - Generated from src/ - DO NOT EDIT DIRECTLY */
/* Run: make plugins */

// === _00_helpers.js ===
/**
 * Calendar Collection Helpers
 */

const CalendarHelpers = {
  SOURCES: {
    google: 'Google',
    outlook: 'Outlook',
    ical: 'iCal',
    manual: 'Manual',
  },

  labelToId(label) {
    for (const [id, l] of Object.entries(this.SOURCES)) {
      if (l.toLowerCase() === label.toLowerCase()) return id;
    }
    return label.toLowerCase();
  },

  idToLabel(id) {
    return this.SOURCES[id] || id;
  },

  sourceMatches(record, targetLabel) {
    const sourceId = record.prop('source')?.choice();
    if (!sourceId) return false;
    const targetId = this.labelToId(targetLabel);
    return sourceId.toLowerCase() === targetId.toLowerCase();
  },

  formatEvent(record, includeDetails = false) {
    const result = {
      guid: record.guid,
      title: record.getName(),
      source: this.idToLabel(record.prop('source')?.choice()),
      calendar: record.text('calendar_name'),
      start_time: record.prop('start_time')?.date()?.toISOString(),
      end_time: record.prop('end_time')?.date()?.toISOString(),
      all_day: record.prop('all_day')?.bool() || false,
    };

    if (includeDetails) {
      result.location = record.text('location');
      result.meeting_url = record.text('meeting_url');
      result.attendees = record.text('attendees');
      result.recurring = record.prop('recurring')?.bool() || false;
    }

    return result;
  },

  isUpcoming(record) {
    const start = record.prop('start_time')?.date();
    return start && start > new Date();
  },

  isPast(record) {
    const end = record.prop('end_time')?.date() || record.prop('start_time')?.date();
    return end && end < new Date();
  },

  isToday(record) {
    const start = record.prop('start_time')?.date();
    if (!start) return false;
    const today = new Date();
    return start.toDateString() === today.toDateString();
  },
};

// === _10_tools.js ===
/**
 * Calendar Collection Tools
 */

const CalendarTools = {
  async getCollection(data) {
    const collections = await data.getAllCollections();
    return collections.find((c) => c.getName() === 'Calendar');
  },

  async upcoming(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Calendar collection not found' };

    let records = await collection.getAllRecords();
    records = records.filter((r) => CalendarHelpers.isUpcoming(r));

    if (args.source) {
      records = records.filter((r) => CalendarHelpers.sourceMatches(r, args.source));
    }

    // Sort by start_time ascending
    records.sort((a, b) => {
      const dateA = a.prop('start_time')?.date() || new Date(0);
      const dateB = b.prop('start_time')?.date() || new Date(0);
      return dateA - dateB;
    });

    const limit = args.limit || 10;
    return records.slice(0, limit).map((r) => CalendarHelpers.formatEvent(r, true));
  },

  async today(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Calendar collection not found' };

    let records = await collection.getAllRecords();
    records = records.filter((r) => CalendarHelpers.isToday(r));

    // Sort by start_time ascending
    records.sort((a, b) => {
      const dateA = a.prop('start_time')?.date() || new Date(0);
      const dateB = b.prop('start_time')?.date() || new Date(0);
      return dateA - dateB;
    });

    return records.map((r) => CalendarHelpers.formatEvent(r, true));
  },

  async find(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Calendar collection not found' };

    let records = await collection.getAllRecords();

    if (args.source) {
      records = records.filter((r) => CalendarHelpers.sourceMatches(r, args.source));
    }
    if (args.calendar) {
      const calLower = args.calendar.toLowerCase();
      records = records.filter((r) =>
        r.text('calendar_name')?.toLowerCase().includes(calLower)
      );
    }
    if (args.location) {
      const locLower = args.location.toLowerCase();
      records = records.filter((r) =>
        r.text('location')?.toLowerCase().includes(locLower)
      );
    }
    if (args.attendee) {
      const attLower = args.attendee.toLowerCase();
      records = records.filter((r) =>
        r.text('attendees')?.toLowerCase().includes(attLower)
      );
    }

    // Sort by start_time descending
    records.sort((a, b) => {
      const dateA = a.prop('start_time')?.date() || new Date(0);
      const dateB = b.prop('start_time')?.date() || new Date(0);
      return dateB - dateA;
    });

    const limit = args.limit || 20;
    return records.slice(0, limit).map((r) => CalendarHelpers.formatEvent(r, true));
  },

  async search(args, data) {
    if (!args.query) return { error: 'Query required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Calendar collection not found' };

    const records = await collection.getAllRecords();
    const queryLower = args.query.toLowerCase();

    let results = records.filter((r) => {
      const title = r.getName()?.toLowerCase() || '';
      const location = r.text('location')?.toLowerCase() || '';
      const attendees = r.text('attendees')?.toLowerCase() || '';
      return title.includes(queryLower) || location.includes(queryLower) || attendees.includes(queryLower);
    });

    const limit = args.limit || 10;
    return results.slice(0, limit).map((r) => CalendarHelpers.formatEvent(r, true));
  },

  async range(args, data) {
    if (!args.start || !args.end) return { error: 'Start and end dates required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Calendar collection not found' };

    const startDate = new Date(args.start);
    const endDate = new Date(args.end);

    const records = await collection.getAllRecords();
    const results = records.filter((r) => {
      const eventStart = r.prop('start_time')?.date();
      if (!eventStart) return false;
      return eventStart >= startDate && eventStart <= endDate;
    });

    // Sort by start_time ascending
    results.sort((a, b) => {
      const dateA = a.prop('start_time')?.date() || new Date(0);
      const dateB = b.prop('start_time')?.date() || new Date(0);
      return dateA - dateB;
    });

    return results.map((r) => CalendarHelpers.formatEvent(r, true));
  },
};

// === _99_plugin.js ===
/**
 * Calendar Collection Plugin
 *
 * Events and meetings from all your calendars.
 * Sources: Google Calendar, Outlook, iCal, Manual
 */
class Plugin extends CollectionPlugin {
  async onLoad() {
    window.addEventListener('synchub-ready', () => this.registerTools(), { once: true });
    if (window.syncHub) this.registerTools();
  }

  registerTools() {
    if (!window.syncHub?.registerCollectionTools) return;

    window.syncHub.registerCollectionTools({
      collection: 'Calendar',
      version: this.getConfiguration().ver || 1,
      description: 'Events and meetings from all your calendars',
      schema: {
        title: 'Event title',
        source: 'Google | Outlook | iCal | Manual',
        calendar_name: 'Calendar name',
        start_time: 'Event start time',
        end_time: 'Event end time',
        all_day: 'All day event',
        location: 'Event location',
        meeting_url: 'Video meeting URL',
        attendees: 'Event attendees',
        recurring: 'Is recurring event',
        tags: 'User tags',
      },
      tools: [
        {
          name: 'upcoming',
          description: 'Get upcoming events. Returns GUIDs.',
          parameters: {
            source: {
              type: 'string',
              enum: ['Google', 'Outlook', 'iCal', 'Manual'],
              optional: true,
            },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => CalendarTools.upcoming(args, data),
        },
        {
          name: 'today',
          description: "Get today's events. Returns GUIDs.",
          parameters: {},
          handler: async (args, data) => CalendarTools.today(args, data),
        },
        {
          name: 'find',
          description: 'Find events by source, calendar, location, or attendee. Returns GUIDs.',
          parameters: {
            source: {
              type: 'string',
              enum: ['Google', 'Outlook', 'iCal', 'Manual'],
              optional: true,
            },
            calendar: { type: 'string', description: 'Calendar name', optional: true },
            location: { type: 'string', description: 'Event location', optional: true },
            attendee: { type: 'string', description: 'Attendee name or email', optional: true },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => CalendarTools.find(args, data),
        },
        {
          name: 'search',
          description: 'Search events by text. Returns GUIDs.',
          parameters: {
            query: { type: 'string', description: 'Search text' },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => CalendarTools.search(args, data),
        },
        {
          name: 'range',
          description: 'Get events within a date range. Returns GUIDs.',
          parameters: {
            start: { type: 'string', description: 'Start date (ISO format)' },
            end: { type: 'string', description: 'End date (ISO format)' },
          },
          handler: async (args, data) => CalendarTools.range(args, data),
        },
      ],
    });

    console.log('[Calendar] Registered collection tools');
  }
}

