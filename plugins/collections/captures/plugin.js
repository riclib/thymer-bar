/* Captures v1 - Generated from src/ - DO NOT EDIT DIRECTLY */
/* Run: make plugins */

// === _00_helpers.js ===
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


// === _10_tools.js ===
/**
 * Captures Collection Tools
 */

const CapturesTools = {
  async getCollection(data) {
    const collections = await data.getAllCollections();
    return collections.find((c) => c.getName() === 'Captures');
  },

  async find(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    let records = await collection.getAllRecords();

    if (args.source) {
      records = records.filter((r) => CapturesHelpers.sourceMatches(r, args.source));
    }
    if (args.author) {
      const authorLower = args.author.toLowerCase();
      records = records.filter((r) =>
        r.text('author')?.toLowerCase().includes(authorLower)
      );
    }
    if (args.site) {
      const siteLower = args.site.toLowerCase();
      records = records.filter((r) =>
        r.text('site')?.toLowerCase().includes(siteLower)
      );
    }

    const limit = args.limit || 20;
    return records.slice(0, limit).map((r) => CapturesHelpers.formatCapture(r));
  },

  async search(args, data) {
    if (!args.query) return { error: 'Query required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    const records = await collection.getAllRecords();
    const queryLower = args.query.toLowerCase();

    let results = records.filter((r) => {
      const title = r.getName()?.toLowerCase() || '';
      const excerpt = r.text('excerpt')?.toLowerCase() || '';
      const site = r.text('site')?.toLowerCase() || '';
      return title.includes(queryLower) || excerpt.includes(queryLower) || site.includes(queryLower);
    });

    const limit = args.limit || 10;
    return results.slice(0, limit).map((r) => CapturesHelpers.formatCapture(r, true));
  },

  async recent(args, data) {
    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    let records = await collection.getAllRecords();

    if (args.source) {
      records = records.filter((r) => CapturesHelpers.sourceMatches(r, args.source));
    }

    // Sort by captured_at descending
    records.sort((a, b) => {
      const dateA = a.prop('captured_at')?.date() || new Date(0);
      const dateB = b.prop('captured_at')?.date() || new Date(0);
      return dateB - dateA;
    });

    const limit = args.limit || 10;
    return records.slice(0, limit).map((r) => CapturesHelpers.formatCapture(r, true));
  },

  async bySite(args, data) {
    if (!args.site) return { error: 'Site required' };

    const collection = await this.getCollection(data);
    if (!collection) return { error: 'Captures collection not found' };

    const records = await collection.getAllRecords();
    const siteLower = args.site.toLowerCase();

    const results = records.filter((r) =>
      r.text('site')?.toLowerCase().includes(siteLower)
    );

    return {
      site: args.site,
      count: results.length,
      captures: results.map((r) => CapturesHelpers.formatCapture(r, true)),
    };
  },
};


// === _99_plugin.js ===
/**
 * Captures Collection Plugin
 *
 * Web pages, highlights, and bookmarks from any source.
 * Sources: Web (llog url), Readwise, Telegram, Kindle, Manual
 */
class Plugin extends CollectionPlugin {
  async onLoad() {
    // Wait for SyncHub to register tools
    window.addEventListener('synchub-ready', () => this.registerTools(), { once: true });
    if (window.syncHub) this.registerTools();
  }

  registerTools() {
    if (!window.syncHub?.registerCollectionTools) return;

    window.syncHub.registerCollectionTools({
      collection: 'Captures',
      version: this.getConfiguration().ver || 1,
      description: 'Web pages, highlights, and bookmarks from any source',
      schema: {
        title: 'Page title or highlight text',
        source: 'Web | Readwise | Telegram | Kindle | Manual',
        site: 'Website domain',
        author: 'Author name',
        url: 'Source URL',
        excerpt: 'Page excerpt or highlight',
        captured_at: 'When captured',
        published_at: 'When published',
        tags: 'User tags',
      },
      tools: [
        {
          name: 'find',
          description: 'Find captures by source, author, or site. Returns GUIDs.',
          parameters: {
            source: {
              type: 'string',
              enum: ['Web', 'Readwise', 'Telegram', 'Kindle', 'Manual'],
              optional: true,
            },
            author: { type: 'string', description: 'Author name', optional: true },
            site: { type: 'string', description: 'Website domain', optional: true },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => CapturesTools.find(args, data),
        },
        {
          name: 'search',
          description: 'Search captures by text. Returns GUIDs.',
          parameters: {
            query: { type: 'string', description: 'Search text' },
            limit: { type: 'number', optional: true },
          },
          handler: async (args, data) => CapturesTools.search(args, data),
        },
        {
          name: 'recent',
          description: 'Get recent captures. Returns GUIDs.',
          parameters: {
            limit: { type: 'number', optional: true },
            source: {
              type: 'string',
              enum: ['Web', 'Readwise', 'Telegram', 'Kindle', 'Manual'],
              optional: true,
            },
          },
          handler: async (args, data) => CapturesTools.recent(args, data),
        },
        {
          name: 'by_site',
          description: 'Get all captures from a specific site. Returns GUIDs.',
          parameters: {
            site: { type: 'string', description: 'Website domain' },
          },
          handler: async (args, data) => CapturesTools.bySite(args, data),
        },
      ],
    });

    console.log('[Captures] Registered collection tools');
  }
}


