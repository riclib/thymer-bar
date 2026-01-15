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
