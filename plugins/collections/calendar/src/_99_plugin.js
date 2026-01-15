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
