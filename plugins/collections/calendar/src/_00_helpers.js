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
