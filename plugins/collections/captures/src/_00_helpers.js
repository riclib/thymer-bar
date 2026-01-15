/**
 * Captures Collection Helpers
 */

const CapturesHelpers = {
  // Source ID/Label mapping
  SOURCES: {
    web: 'Web',
    readwise: 'Readwise',
    telegram: 'Telegram',
    kindle: 'Kindle',
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

  // Format capture for API response
  formatCapture(record, includeContent = false) {
    const result = {
      guid: record.guid,
      title: record.getName(),
      source: this.idToLabel(record.prop('source')?.choice()),
      site: record.text('site'),
      author: record.text('author'),
      url: record.text('url'),
      captured_at: record.prop('captured_at')?.date()?.toISOString(),
    };

    if (includeContent) {
      result.excerpt = record.text('excerpt');
    }

    return result;
  },
};
